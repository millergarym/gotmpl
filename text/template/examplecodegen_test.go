// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package template_test

import (
	"fmt"
	"go/format"
	"log"
	"sort"
	"strings"

	"github.com/millergarym/gotmpl/text/template"
)

// generator is the dynamically scoped "global" threaded through every template
// invocation as $G. It carries two things the templates below need no matter
// how deeply they recurse:
//
//   - TagFields, a flag that switches struct-tag emission on or off; and
//   - the set of imported packages, accumulated as a side effect of rendering.
//
// The catch is that during recursion the template's dot is taken up by the
// data being rendered (a []fieldDef), so there is nowhere to also pass the
// generator. Dynamic scoping is what lets the "field" template still reach $G.
type generator struct {
	Package   string
	TagFields bool // emit `json:"..."` struct tags
	Structs   []structDef

	imports map[string]string // import path -> package qualifier
}

// Import records an import path and returns the qualifier used to reference it,
// e.g. Import("net/http") registers the import and returns "http".
func (g *generator) Import(path string) string {
	qual := path[strings.LastIndex(path, "/")+1:]
	if g.imports == nil {
		g.imports = make(map[string]string)
	}
	g.imports[path] = qual
	return qual
}

// Imports returns the recorded import paths, sorted for deterministic output.
func (g *generator) Imports() []string {
	paths := make([]string, 0, len(g.imports))
	for path := range g.imports {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

type structDef struct {
	Name   string
	Doc    string
	Fields []fieldDef
}

// GoDoc renders s.Doc as a doc comment, or "" if there is none.
func (s structDef) GoDoc() string {
	if s.Doc == "" {
		return ""
	}
	return "// " + s.Name + " " + s.Doc
}

type fieldDef struct {
	Name   string
	Type   string     // scalar Go type name, unqualified; empty when nested
	Pkg    string     // import path Type comes from, "" for a builtin
	Fields []fieldDef // non-empty => an anonymous nested struct
}

// Nested reports whether the field is itself an anonymous struct.
func (f fieldDef) Nested() bool { return len(f.Fields) > 0 }

// GoTag renders the struct tag for the field, deriving the JSON key from the
// field name.
func (f fieldDef) GoTag() string {
	return fmt.Sprintf("`json:%q`", strings.ToLower(f.Name))
}

// This example uses a set of templates to generate Go source code. A single
// generator value is bound once as a dynamically scoped variable ($G); the
// deeply nested, mutually recursive templates reach it to collect imports and
// to honour the TagFields flag, without it ever being the template's dot.
func ExampleTemplate_codegen() {
	// structbody emits "struct {" and its matching "}" around a *single* range
	// over the fields. Because a field may itself be a nested struct, "field"
	// recurses back into "structbody" -- so one template produces perfectly
	// balanced braces at every depth. In stock text/template you cannot write
	// this: "field" references $G with no lexical binding, so the parser
	// rejects it as an undefined variable. It parses here only because
	// WithDynamicScopedVars enables parse.DynScopedVars.
	const source = `
{{- define "body" -}}
{{- $G := . -}}{{- /* create a global variable */ -}}
{{range .Structs -}}
{{template "typedecl" .}}

{{end -}}
{{- end -}}

{{- define "typedecl" -}}
{{.GoDoc}}{{/* method call*/}}
type {{.Name}} {{template "structbody" .Fields}}
{{- end -}}

{{- define "structbody" -}}
struct {
{{range . -}}
{{template "field" .}}
{{end -}}
}
{{- end -}}

{{- define "field" -}}
{{.Name}}{{" "}}
{{- if .Nested -}}
{{template "structbody" .Fields}}{{/* recurse */}}
{{- else -}}
{{if .Pkg}}{{$G.Import .Pkg}}.{{end}}{{.Type}}{{/* $G.Import method call with side effects */}}
{{- end -}}
{{if $G.TagFields}} {{.GoTag}}{{end}}{{/* global switch */}}
{{- end -}}

{{- define "header" -}}
package {{.Package}}
{{if .Imports}}{{/* method call*/}}
import (
{{range .Imports -}}{{- /* method call*/ -}}
"{{.}}"
{{end -}}
)
{{end}}
{{- end -}}
`

	g := &generator{
		Package:   "models",
		TagFields: true,
		Structs: []structDef{
			{
				Name: "Event",
				Doc:  "is a single recorded happening.",
				Fields: []fieldDef{
					{Name: "When", Type: "Time", Pkg: "time"},
					{Name: "Elapsed", Type: "Duration", Pkg: "time"},
					{Name: "Meta", Fields: []fieldDef{
						{Name: "Source", Type: "string"},
						{Name: "Tags", Type: "[]string"},
						{Name: "Seen", Type: "Time", Pkg: "time"},
					}},
					{Name: "Body", Type: "Buffer", Pkg: "bytes"},
				},
			},
			{
				Name: "Request",
				Fields: []fieldDef{
					{Name: "HTTP", Type: "Request", Pkg: "net/http"},
					{Name: "Received", Type: "Time", Pkg: "time"},
					{Name: "ID", Type: "int64"},
				},
			},
		},
	}

	// Enable dynamic scoping so $G, bound in "body", is visible to every
	// template it (transitively) invokes.
	tmpl := template.Must(template.New(
		"gen",
		template.WithDynamicScopedVars(), // <-- opt-in to DSV
	).Parse(source))

	// Render the body first: walking the structs records, as a side effect,
	// every package referenced by a field into the generator.
	var body strings.Builder
	if err := tmpl.ExecuteTemplate(&body, "body", g); err != nil {
		log.Fatal(err)
	}

	// Now the header can render the import block from the collected imports.
	var header strings.Builder
	if err := tmpl.ExecuteTemplate(&header, "header", g); err != nil {
		log.Fatal(err)
	}

	// Assemble and gofmt the result so the output is canonical Go source.
	out, err := format.Source([]byte(header.String() + body.String()))
	if err != nil {
		log.Fatalf("format source: %v", err)
	}
	fmt.Printf("%s", out)

	// Output:
	// package models
	//
	// import (
	// 	"bytes"
	// 	"net/http"
	// 	"time"
	// )
	//
	// // Event is a single recorded happening.
	// type Event struct {
	// 	When    time.Time     `json:"when"`
	// 	Elapsed time.Duration `json:"elapsed"`
	// 	Meta    struct {
	// 		Source string    `json:"source"`
	// 		Tags   []string  `json:"tags"`
	// 		Seen   time.Time `json:"seen"`
	// 	} `json:"meta"`
	// 	Body bytes.Buffer `json:"body"`
	// }
	//
	// type Request struct {
	// 	HTTP     http.Request `json:"http"`
	// 	Received time.Time    `json:"received"`
	// 	ID       int64        `json:"id"`
	// }
}
