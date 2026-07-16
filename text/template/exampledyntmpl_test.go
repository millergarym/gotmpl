// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package template_test

import (
	"fmt"
	"log"
	"strings"

	"github.com/millergarym/gotmpl/text/template"
)

func ExampleTemplate_dynamic_template() {
	const source = `
{{$name := concat .Type1 "body"}}{{template $name .}}
{{$name = concat .Type2 "body"}}{{template $name .}}
{{- define "Abody" -}}Abody{{- end -}}
{{- define "Bbody" -}}Bbody{{- end -}}

`
	tmpl := template.Must(template.New("root").
		Funcs(template.FuncMap{
			"concat": func(s ...string) string {
				return strings.Join(s, "")
			},
		}).
		Parse(source),
	)
	data := struct {
		Type1 string
		Type2 string
	}{
		"A", "B",
	}
	var body strings.Builder
	if err := tmpl.ExecuteTemplate(&body, "root", data); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%s", body.String())

	// Output:
	// Abody
	// Bbody
}

type A struct {
	F1 string
}
type B struct {
	F2 string
}

func ExampleTemplate_tmpl_by_typename() {
	const source = `
{{tmpl_by_typename .Type1 "__" "body"}}
{{tmpl_by_typename .Type2 "me" "body"}}
{{- define "__Abody" -}}Abody {{.F1}}{{- end -}}
{{- define "meBbody" -}}Bbody {{.F2}}{{- end -}}
`
	tmpl := template.Must(template.New("root").Parse(source))
	data := struct {
		Type1 A
		Type2 B
	}{
		A{"an a"},
		B{"a b"},
	}
	var body strings.Builder
	if err := tmpl.ExecuteTemplate(&body, "root", data); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%s", body.String())

	// Output:
	// Abody an a
	// Bbody a b
}
