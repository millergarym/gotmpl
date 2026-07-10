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
// invocation. As the body templates render they call Import to record the
// packages they need; the header template then emits a matching import block.
//
// The point of the example is that Import is reached from templates several
// levels below where the generator is bound (file -> struct -> field), without
// the generator ever being passed as the template's dot. Dynamic scoping makes
// the $G binding visible to invoked templates.
type generator struct {
	Package string
	Structs []structDef

	imports map[string]string // import path -> package qualifier
}

type structDef struct {
	Name   string
	Fields []fieldDef
}

type fieldDef struct {
	Name string
	Type string // Go type name, unqualified
	Pkg  string // import path the type comes from, or "" for a builtin
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

// This example uses a set of templates to generate Go source code. A single
// generator value is bound once as a dynamically scoped variable ($G) and used
// by deeply nested templates to collect the set of imported packages, which is
// then rendered into the file's import block.
func ExampleTemplate_codegen() {
	const source = `
{{- define "field" -}}
	{{.Name}} {{if .Pkg}}{{$G.Import .Pkg}}.{{end}}{{.Type}}
{{- end -}}

{{- define "struct" -}}
type {{.Name}} struct {
{{range .Fields}}	{{template "field" .}}
{{end -}}
}
{{end -}}

{{- define "body" -}}
	{{$G := .}}
	{{- range .Structs}}{{template "struct" .}}
{{end -}}
{{- end -}}

{{- define "header" -}}
package {{.Package}}
{{if .Imports}}
import (
{{range .Imports}}	"{{.}}"
{{end -}}
)
{{end}}
{{- end -}}
`

	g := &generator{
		Package: "models",
		Structs: []structDef{
			{
				Name: "Event",
				Fields: []fieldDef{
					{Name: "When", Type: "Time", Pkg: "time"},
					{Name: "Elapsed", Type: "Duration", Pkg: "time"},
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

	// Enable dynamic scoping so $G, bound in "body", is visible to the
	// "struct" and "field" templates it invokes.
	tmpl := template.Must(template.New("gen", template.WithDynamicScopedVars()).Parse(source))

	// Render the body first: this walks the structs and, as a side effect,
	// records every package referenced by a field into the generator.
	var body strings.Builder
	if err := tmpl.ExecuteTemplate(&body, "body", g); err != nil {
		log.Fatal(err)
	}

	// Now the header can render the import block from the collected imports.
	var header strings.Builder
	if err := tmpl.ExecuteTemplate(&header, "header", g); err != nil {
		log.Fatal(err)
	}

	// // Assemble and gofmt the result so the output is canonical Go source.
	out, err := format.Source([]byte(header.String() + body.String()))
	if err != nil {
		log.Fatalf("format source err: '%v'", err)
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
	// type Event struct {
	// 	When    time.Time
	// 	Elapsed time.Duration
	// 	Body    bytes.Buffer
	// }
	//
	// type Request struct {
	// 	HTTP     http.Request
	// 	Received time.Time
	// 	ID       int64
	// }
}
