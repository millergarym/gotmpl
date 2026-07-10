# gotmpl — Go templates with dynamic scoped variables

`gotmpl` is a fork/drop-in replacement of the Go standard library's [`text/template`](https://pkg.go.dev/text/template)
and [`html/template`](https://pkg.go.dev/html/template) packages, copied from the Go
source at **go1.26.4**. It adds one feature on top of the stock behaviour: an opt-in
**dynamic scoped variables** mode.


[Code-gen example in Go Playground](https://go.dev/play/p/f-fyWd3TTIB)\
[Simple Example](https://go.dev/play/p/AP6KJ3jf_7j):
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

Everything the standard packages does continues to work unchanged; the new behaviour is
off by default and must be explicitly enabled.

## The problem

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


## Dynamic scoped variables

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

### Semantics

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

### Parser behaviour

Enabling dynamic scoping also relaxes the parser's *variable-is-defined* check. Normally
the parser rejects a template that references a variable which is not lexically in scope.
Under dynamic scoping the variable may legitimately be supplied by a caller, so the check
is skipped (`parse.DynScopedVars` mode). Templates are therefore validated at parse time
only for syntax, not for variable resolution.

## Enabling it

There are three equivalent ways to turn the feature on:

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

## Use cases

Code generation is where dynamic scoping earns its keep. A generator is naturally a tree
of small, composable templates — a file invokes a type, a type invokes its fields, a field
may invoke a nested type — and `dot` at each level is busy carrying *the thing being
rendered*. Yet almost every generator also needs some ambient, whole-file state that every
level must reach: an import collector, an indentation level, naming/casing options, a
symbol table, feature flags. In stock Go templates that state can only travel through
`dot`, so you either wrap every value in a context struct and thread it through by hand, or
you flatten the templates to keep everything in one scope. Dynamic scoping removes the
choice: bind the state once, reach it anywhere below.

Concretely, dynamic scoping helps because:

- **Ambient state without plumbing.** Bind `{{$G := .}}` once in the outermost template
  and every invoked template — however deep — can read and mutate it. No context struct,
  no passing `$G` through templates that don't themselves use it.
- **`dot` stays free for data.** The recursion payload (a field, a sub-type, a list tail)
  can own `dot` while shared services ride along in `$G`. The two concerns stop fighting
  over the one slot.
- **Open and close in one template.** Emitting matched delimiters — `struct { … }`,
  `func … { … }`, `[ … ]`, indentation push/pop — no longer forces you to split the
  opening and closing into two separate ranges. A single recursive template writes the
  open token, recurses, then writes the close, so braces stay balanced by construction at
  every depth.
- **Collect-as-you-render.** Side effects like recording an import or bumping a counter
  happen at the exact point of use, deep in the tree, and are visible to the caller
  afterwards — the pattern used for the import block below.
- **Per-run options travel with the value.** Casing rules, tag styles, or feature flags
  set on the generator are readable everywhere without being re-declared per template.

### Collecting imports during code generation

The motivating use case is generating source code. When rendering a file, the templates
that emit individual fields or expressions discover which packages need importing — but
the `import` block lives at the top of the file, far from where those decisions are made,
and usually several template invocations away.

Dynamic scoping lets you bind a single "generator" value once and reach it from every
template below, so nested templates can register their imports as a side effect of
rendering. `dot` stays free to carry the data being rendered.

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

- `dynamicScopedVars` option and the `WithDynamicScopedVars` / `WithMissingKeyAction`
  functional options.
- `parse.DynScopedVars` mode plus `parse.ParseWithOptions`, `parse.WithFuncs`, and
  `parse.WithMode`.
- `New`, `ParseGlob`, and friends accept functional `Option`s.

## Testing

```sh
go test ./...
```

The dynamic-scoping behaviour is covered by `TestDynamicScopedVars001`–`003`,
`ExampleTemplate_dynamicvars`, and the code-generation example
[`ExampleTemplate_codegen`](text/template/examplecodegen_test.go) in `text/template`.

## License

BSD-style, as inherited from the Go project. See [LICENSE](LICENSE).
