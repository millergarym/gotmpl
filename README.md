# gotmpl — Go templates with dynamic scoped variables

`gotmpl` is a fork of the Go standard library's [`text/template`](https://pkg.go.dev/text/template)
and [`html/template`](https://pkg.go.dev/html/template) packages, copied from the Go
source at **go1.26.4**. It adds one feature on top of the stock behaviour: an opt-in
**dynamic scoped variables** mode.

```go
import (
    "github.com/millergarym/gotmpl/text/template"
    "github.com/millergarym/gotmpl/html/template"
)
```

Everything the standard packages do continues to work unchanged; the new behaviour is
off by default and must be explicitly enabled.

## The problem

In standard Go templates, variables (`{{$x := ...}}`) are **lexically scoped**. A
variable is only visible within the template block that declares it. When one template
invokes another with `{{template "name"}}`, the invoked template starts with a fresh
scope containing only `$` (the data value, `dot`) — none of the caller's variables carry
over.

That means there is no way to declare a value once in an outer template and reference it
from the templates it invokes; you have to thread it through the `dot` value passed to
each `{{template}}` call.

## Dynamic scoped variables

When dynamic scoping is enabled, variables declared in a calling template become visible
to the templates it invokes:

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
  recent binding for a given name is propagated:

  ```go
  // T1 overrides $F before invoking T2
  `{{$F := .}}...({{template "T1"}})
  {{- define "T1"}}{{$F := "override"}}...({{template "T2"}}){{end -}}
  {{- define "T2"}}F = {{$F}}{{end -}}`
  // → F = override
  ```

- Because the caller's variables stay in scope, recursive templates can carry mutable
  state through a pointer value:

  ```go
  `{{$F := .}}({{template "T1"}})
  {{- define "T1"}}{{if $F.More}}{{$F.To}} ({{template "T1"}}){{else}}done{{end}}{{end -}}`
  // with &global{To: 3} → (2 (1 (0 (done))))
  ```

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

The dynamic-scoping behaviour is covered by `TestDynamicScopedVars001`–`003` and
`ExampleTemplate_dynamicvars` in `text/template`.

## License

BSD-style, as inherited from the Go project. See [LICENSE](LICENSE).
