package app

import (
	"bytes"
	"text/template"
)

func TraceInfo() string {
	var tpl = template.Must(template.New("info").Parse(`
AppName     {{.Name}}
Version     {{.FullVersion}}
{{if .Git.Trace}}
GitTrace    {{.Git.Trace}}{{if .Git.Branch}}
GitBranch   {{.Git.Branch}}{{end}}{{if .Git.Repo}}
GitRepo     {{.Git.Repo}}{{end}}
GitHash     {{.Git.CommitHash}} @ {{.Git.CommitTimeString}}
{{end}}
Golang      {{.Go.Version}} {{.Go.Arch}}
BuildInfo   {{.Build.ID}} @ {{.Build.TimeString}}
`))

	var buf bytes.Buffer
	if err := tpl.Execute(&buf, App); err != nil {
		panic(err)
	}
	buf.Next(1) // trim first \n
	return buf.String()
}
