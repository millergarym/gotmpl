# gotmpl — Go templates with dynamic scoped variables

`gotmpl` is a fork/drop-in replacement of the Go standard library's [`text/template`](https://pkg.go.dev/text/template)
and [`html/template`](https://pkg.go.dev/html/template) packages, copied from the Go
source at **go1.26.4**. It adds a handful of features on top of the stock behaviour,
aimed at code generation:

- **[`tmpl_by_type`](#tmpl_by_type--dispatch-on-the-values-type)** — invoke a template
  chosen from the runtime *type* of a value.
- **[Dynamic template names](#dynamic-template-names)** — `{{template $name}}`, where the
  template to invoke is held in a variable.
- **[Void functions](#void-functions)** — `FuncMap` functions with no return value, for
  side effects like collecting state.
- **[Dynamic scoped variables](#dynamic-scoped-variables)** — an opt-in mode where a
  calling template's variables are visible to the templates it invokes.

Everything the standard packages do continues to work unchanged; each new feature is
additive and, where it changes evaluation, off by default.

## Examples

### `tmpl_by_type` — dispatch on the value's type

`{{tmpl_by_type pipeline "prefix" "suffix"}}` invokes a template whose name is built from
the **runtime type name** of the pipeline's value, wrapped in the string constants
`prefix` and `suffix`. The value becomes `dot` in the invoked template.

```go
type A struct{ F1 string }
type B struct{ F2 string }

const src = `
{{tmpl_by_type .Type1 "__" "body"}}
{{tmpl_by_type .Type2 "me" "body"}}
{{- define "__Abody" -}}Abody {{.F1}}{{- end -}}
{{- define "meBbody" -}}Bbody {{.F2}}{{- end -}}
`
tmpl := template.Must(template.New("root").Parse(src))
data := struct {
    Type1 A
    Type2 B
}{A{"an a"}, B{"a b"}}

_ = tmpl.ExecuteTemplate(os.Stdout, "root", data)
// Output:
// Abody an a
// Bbody a b
```

Here `.Type1` is an `A`, so `tmpl_by_type` resolves the template name `"__" + "A" +
"body"` = `"__Abody"`; `.Type2` is a `B`, resolving to `"meBbody"`. This lets a generator
render a heterogeneous value by dispatching to a per-type template without a manual
`if`/`else` chain — see
[`ExampleTemplate_tmpl_by_type`](text/template/exampledyntmpl_test.go). Both operands
must be string constants; the value must have a named, non-nil type.

### Dynamic template names

The template name in a `{{template}}` action may be a **variable** instead of a string
constant. The variable is resolved at execution time and its value used as the template
name, so the callee can be chosen at runtime:

```go
const src = `
{{$name := concat .Type1 "body"}}{{template $name .}}
{{$name = concat .Type2 "body"}}{{template $name .}}
{{- define "Abody" -}}Abody{{- end -}}
{{- define "Bbody" -}}Bbody{{- end -}}
`
tmpl := template.Must(template.New("root").
    Funcs(template.FuncMap{
        "concat": func(s ...string) string { return strings.Join(s, "") },
    }).
    Parse(src),
)
data := struct{ Type1, Type2 string }{"A", "B"}

_ = tmpl.ExecuteTemplate(os.Stdout, "root", data)
// Output:
// Abody
// Bbody
```

The stock parser rejects `{{template $v}}` outright; here it is accepted and the variable
is looked up in scope during execution — see
[`ExampleTemplate_dynamic_template`](text/template/exampledyntmpl_test.go).

### Void functions

`FuncMap` functions may declare **no return value**. In stock Go templates every function
must return one value (or a value and an `error`); here a `func(...)` with zero results is
allowed. Its call produces no output and must be the last command in its pipeline. This
makes side-effecting functions — a setter, a collector, a counter — first-class:

```go
var val any
tmpl, err := template.New("root", template.WithDynamicScopedVars()).
    Funcs(template.FuncMap{
        "set": func(a any) { val = a }, // no return value
        "get": func() any { return val },
    }).Parse(`
{{- set . -}}T0 invokes T1: ({{template "T1"}}){{"\n"}}
{{- define "T1"}}T1 invokes T2: ({{template "T2"}}){{end -}}
{{- define "T2"}}This is T2. F = {{get}}{{end -}}
`)
if err != nil {
    log.Fatal(err)
}
_ = tmpl.Execute(os.Stdout, "hw")
// Output:
// T0 invokes T1: (T1 invokes T2: (This is T2. F = hw))
```

`{{set .}}` records the value and emits nothing; `{{get}}` reads it back two invocations
deeper. Paired with dynamic scoping (or a curried receiver), void functions give templates
a clean way to thread ambient state — see
[`ExampleTemplate_curry_val`](text/template/examplefunc_test.go) and
[`ExampleTemplate_curry_receiver`](text/template/examplefunc_test.go).

### Dynamic Scoped Variables

```go
package main

import (
	"log"
	"os"

	"github.com/millergarym/gotmpl/text/template"
)

func main() {
	tmpl, err := template.New(
		"root",
		template.WithDynamicScopedVars(), // <-- opt-in
	).
		Parse(`
{{- $F := . -}}T0 invokes T1: ({{template "T1"}}){{"\n"}}
{{- define "T1"}}T1 invokes T2: ({{template "T2"}}){{end -}}
{{- define "T2"}}This is T2. F = {{$F}}{{end -}}
`)
	if err != nil {
		log.Fatal(err)
	}
	tmpl.Execute(os.Stdout, "hw")
}
```

## Playground
[Code-gen example in Go Playground](https://go.dev/play/p/f-fyWd3TTIB)\
[Simple Example](https://go.dev/play/p/AP6KJ3jf_7j):

## The problem: code generation

A code generator is naturally a **tree of small, composable templates** — a file invokes a
type, a type invokes its fields, a field may invoke a nested type — and at every level
`dot` is busy carrying *the thing being rendered*. Stock Go templates make two recurring
demands of such a tree awkward, and gotmpl adds features that address each:

1. **Flow control** — choosing *which* template renders a given value. The stock
   `{{template "name"}}` dispatches only on a name known at parse time, so rendering
   heterogeneous data (a sum type, an AST/IR node, a `oneof`) forces a type switch back in
   Go or a chain of `{{if}}`s keyed on a discriminator field. → addressed by
   [`tmpl_by_type`](#tmpl_by_type--dispatch-on-the-values-type) and
   [dynamic template names](#dynamic-template-names).
2. **Scoping** — sharing *ambient state* across the tree. Variables are lexically scoped,
   so only `dot` crosses a `{{template}}` boundary; whole-file state (an import collector,
   an indentation level, naming options, feature flags) has nowhere to live. → addressed by
   [dynamic scoped variables](#scoping-sharing-ambient-state) and
   [curried `FuncMap` functions](#scoping-sharing-ambient-state).

## Flow control: dispatching on type

To render a value whose concrete type is only known at run time, stock templates give you
one lever: a literal template name. Anything type-dependent has to be decided *outside* the
template — a Go type switch that picks the name, or an `{{if}}`/`{{else}}` ladder over a
tag field — which pulls the generator's structure out of the templates and into code.

gotmpl adds two ways to choose the callee at execution time:

- **`tmpl_by_type`** builds the template name from the *runtime type name* of a value:
  `{{tmpl_by_type .Node "render_" ""}}` invokes `render_IfStmt` for an `IfStmt`,
  `render_CallExpr` for a `CallExpr`, and so on — a type switch expressed as a naming
  convention, with the value handed straight to the chosen template as `dot`. See the
  [example above](#tmpl_by_type--dispatch-on-the-values-type) and
  [`ExampleTemplate_tmpl_by_type`](text/template/exampledyntmpl_test.go).
- **Dynamic template names** are the general escape hatch: compute a name into a variable
  and invoke it with `{{template $name .}}`. Use this when the callee is selected by
  something other than the type — a mode flag, a lookup table, a field value. See the
  [example above](#dynamic-template-names) and
  [`ExampleTemplate_dynamic_template`](text/template/exampledyntmpl_test.go).

Both keep dispatch *in the template tree*, so adding a case is adding a `{{define}}`, not
editing Go and re-running the generator's own build.

## Scoping: sharing ambient state

In standard Go templates, variables (`{{$x := ...}}`) are **lexically scoped**. A
variable is only visible within the template block that declares it. When one template
invokes another with `{{template "name"}}`, the invoked template starts with a fresh
scope containing only `$` (the data value, `dot`) — none of the caller's variables carry
over.

So the only pipeline slot into an invoked template is `dot`. That is fine until you have
*ambient state* that many templates need to share — an import collector, an indentation
level, naming options, a feature flag. There is nowhere to put it, which leaves two
awkward workarounds:

- **Thread it through `dot`.** Wrap the real data and the shared state together in a
  context struct and pass it to every `{{template}}` call — even through templates that
  only relay it. Now `dot` is carrying two things at once, so it is no longer free to be
  the recursion payload (a field, a sub-type, a list tail), and the plumbing spreads to
  templates that have no use for it.
- **Flatten the templates.** Collapse the tree into one big scope to keep everything
  reachable, giving up the small composable templates that make a generator readable.

A knock-on effect shows up with matched delimiters. Because a value computed on the way
*down* the tree can't be seen again on the way back *up*, emitting balanced tokens —
`struct { … }`, `func … { … }`, indentation push/pop — tends to get split across two
separate ranges (one to open, one to close) instead of a single recursive template that
opens, recurses, then closes.

gotmpl offers two complementary ways to give that ambient state a home, both usable on
their own or together:

### Option A — dynamic scoped variables

When dynamic scoping is enabled, variables declared in a calling template become visible
to the templates it invokes (runnable as
[`ExampleTemplate_dynamicvars`](text/template/examplefiles_test.go)):

```go
tmpl, err := template.New("root", template.WithDynamicScopedVars()).Parse(
    `{{$F := .}}T0 invokes T1: ({{template "T1"}})
{{- define "T1"}}T1 invokes T2: ({{template "T2"}}){{end -}}
{{- define "T2"}}This is T2. F = {{$F}}{{end -}}
`)

_ = tmpl.Execute(os.Stdout, "hw")
// Output: T0 invokes T1: (T1 invokes T2: (This is T2. F = hw))
```

Here `$F` is declared in the root template and referenced two levels deeper in `T2`,
without being passed explicitly through the intermediate `T1`.

#### Semantics

- The data value `$` is **not** inherited — each invoked template receives the `dot`
  passed to the `{{template ... .}}` call, exactly as in stock Go templates.
- All other variables in scope at the point of invocation are passed into the invoked
  template.
- An invoked template may **shadow** an inherited variable by redeclaring it; the new
  value applies within that template (and templates it invokes), and only the most
  recent binding for a given name is propagated (see
  [`TestDynamicScopedVars002`](text/template/multi_test.go)):

  ```go
  // T1 overrides $F before invoking T2
  `{{$F := .}}...({{template "T1"}})
  {{- define "T1"}}{{$F := "override"}}...({{template "T2"}}){{end -}}
  {{- define "T2"}}F = {{$F}}{{end -}}`
  // → F = override
  ```

- Because the caller's variables stay in scope, recursive templates can carry mutable
  state through a pointer value (see
  [`TestDynamicScopedVars003`](text/template/multi_test.go)):
  Below `More` is a **method** on the passed in the data variable.
  The invocation of `More`, decrements the `To` field, and return if `g.To >= 0`.
  ```go
  `{{$F := .}}({{template "T1"}})
  {{- define "T1"}}{{if $F.More}}{{$F.To}} ({{template "T1"}}){{else}}done{{end}}{{end -}}`
  // with &global{To: 3} → (2 (1 (0 (done))))
  ```

For complex code-generators methods turn out to be more convenient than functions passed in via the `funcMap`.

#### Parser behaviour

Enabling dynamic scoping also relaxes the parser's *variable-is-defined* check. Normally
the parser rejects a template that references a variable which is not lexically in scope.
Under dynamic scoping the variable may legitimately be supplied by a caller, so the check
is skipped (`parse.DynScopedVars` mode). Templates are therefore validated at parse time
only for syntax, not for variable resolution.

### Option B — curried `FuncMap` functions

Ambient state can also live *outside* the template, captured in the `FuncMap` itself. A
closure over a local, or a method on a receiver, keeps the state in Go; the template just
calls in to read and mutate it. **Void functions** make this ergonomic — a function with
no return value acts as a pure side effect (a setter, a collector, a counter) and emits
nothing:

```go
var val any
funcs := template.FuncMap{
    "set": func(a any) { val = a }, // void: records, emits nothing
    "get": func() any { return val },
}
// ... or curry a receiver, so the state is a struct field:
x := &X{}
funcs = template.FuncMap{"set": x.Set, "get": x.Get}
```

`{{set .}}` deep in the tree writes the shared value and `{{get}}` reads it back
elsewhere, with no `dot` plumbing and no context struct — see
[`ExampleTemplate_curry_val`](text/template/examplefunc_test.go) (closure) and
[`ExampleTemplate_curry_receiver`](text/template/examplefunc_test.go) (method receiver).

**Which to reach for.** Dynamic scoping keeps the state *inside* the template scope, which
reads naturally for values that shadow and recurse (`{{$G := .}}`, then `{{$G.Import ...}}`
anywhere below). Curried funcs keep the state *in Go*, which is handy when the collector is
already a Go object with real methods, or when you'd rather not enable dynamic scoping at
all. They compose: the void-function example above also switches on dynamic scoping so
`get`/`set` can be reached from templates whose `dot` is unrelated.

## Enabling dynamic scoping

Dynamic scoping is the one feature that is off by default (`tmpl_by_type`, dynamic template
names, and void functions are always available). There are three equivalent ways to turn
it on:

**1. Functional option (idiomatic, recommended)**

```go
tmpl := template.New("root", template.WithDynamicScopedVars())

// also available: template.WithMissingKeyAction(...)
```

`WithDynamicScopedVars` can also be passed to `New` and `ParseGlob`.

**2. String option**

```go
tmpl := template.New("root").Option("dynamicScopedVars")
```

**3. Directly at the parser layer**

```go
trees, err := parse.ParseWithOptions(name, text, "{{", "}}",
    parse.WithFuncs(funcs),
    parse.WithMode(parse.DynScopedVars),
)
```

## Worked example: a code generator

The two problems above rarely show up alone — a real generator dispatches on type *and*
threads shared state through the tree. The following examples put the pieces together.

### Collecting imports as you render

When rendering a file, the templates that emit individual fields or expressions discover
which packages need importing — but the `import` block lives at the top of the file, far
from where those decisions are made, and usually several template invocations away. Bind a
single "generator" value once and reach it from every template below, so nested templates
register their imports as a side effect of rendering. `dot` stays free to carry the data
being rendered.

```go
type generator struct {
    imports map[string]bool
}

// Import records a package and returns the qualifier to reference it with,
// e.g. Import("net/http") registers the import and returns "http".
func (g *generator) Import(path string) string {
    if g.imports == nil {
        g.imports = map[string]bool{}
    }
    g.imports[path] = true
    return path[strings.LastIndex(path, "/")+1:]
}

const src = `{{$G := .}}{{template "field" .}}
{{- define "field"}}When {{$G.Import "time"}}.Time{{end -}}`

g := &generator{}
tmpl := template.Must(template.New("gen", template.WithDynamicScopedVars()).Parse(src))
_ = tmpl.Execute(io.Discard, g)

fmt.Println(g.imports) // map[time:true] — collected from inside "field"
```

`$G` is bound in the root template but used inside `"field"`, whose `dot` is not the
generator. Without dynamic scoping this template would not even parse — the parser
rejects `$G` as an undefined variable — which is exactly why the feature exists.

### Recursive Calls

For a fuller, runnable version — recursive nested structs whose matching `struct { … }`
braces are emitted by a *single* template (no splitting the open and close across two
ranges), struct-tag generation gated by a flag on the generator, and a real `import`
block assembled from the collected packages and run through `gofmt` — see
[`ExampleTemplate_codegen`](text/template/examplecodegen_test.go).

## Package layout

| Path                    | Description                                                   |
| ----------------------- | ------------------------------------------------------------- |
| `text/template`         | Text template engine with the dynamic-scoping option.         |
| `text/template/parse`   | Lexer/parser, extended with `ParseWithOptions` and `Mode`.    |
| `html/template`         | HTML templates (contextual autoescaping) built on the above.  |
| `internal/fmtsort`      | Vendored copy of the standard library's map-sorting helper.   |

## Relationship to upstream

The tree tracks the Go standard library so that upstream fixes and the familiar template
syntax remain available. The additions over stock go1.26.4 are:

- The `tmpl_by_type` action and its `parse.TemplateByTypenameNode`.
- Dynamic template names — `{{template $var}}` resolves the callee from a variable.
- Void `FuncMap` functions (zero return values), evaluated as side-effect-only commands.
- `dynamicScopedVars` option and the `WithDynamicScopedVars` / `WithMissingKeyAction`
  functional options.
- `parse.DynScopedVars` mode plus `parse.ParseWithOptions`, `parse.WithFuncs`, and
  `parse.WithMode`.
- `New`, `ParseGlob`, and friends accept functional `Option`s.

## Testing

```sh
go test ./...
```

The added behaviour is covered in `text/template` by:

- Dynamic scoping — `TestDynamicScopedVars001`–`003`, `ExampleTemplate_dynamicvars`, and
  the code-generation example
  [`ExampleTemplate_codegen`](text/template/examplecodegen_test.go).
- `tmpl_by_type` and dynamic template names —
  [`ExampleTemplate_tmpl_by_type`](text/template/exampledyntmpl_test.go) and
  [`ExampleTemplate_dynamic_template`](text/template/exampledyntmpl_test.go).
- Void / curried functions —
  [`ExampleTemplate_curry_val`](text/template/examplefunc_test.go) and
  [`ExampleTemplate_curry_receiver`](text/template/examplefunc_test.go).

## License

BSD-style, as inherited from the Go project. See [LICENSE](LICENSE).
