package main

import "text/template"

type TemplateData struct {
	Host          string
	Password      string
	DumpDirectory string
}

var pythonScriptTemplate = template.Must(template.New("").Parse(`shell.connect("root@{{.Host}}", "{{.Password}}")
util.dump_instance('{{.DumpDirectory}}')
`))
