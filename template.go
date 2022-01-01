package main

import "text/template"

type TemplateData struct {
	Host          string
	User          string
	Password      string
	DumpDirectory string
}

var pythonScriptTemplate = template.Must(template.New("").Parse(`shell.connect("{{.User}}@{{.Host}}", "{{.Password}}")
util.dump_instance('{{.DumpDirectory}}')
`))
