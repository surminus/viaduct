package main

import (
	"embed"

	"github.com/surminus/viaduct"
)

//go:embed templates
var templates embed.FS

func main() {
	viaduct.Directory{Path: "test"}.Create()

	viaduct.Template{
		Path:      "test/foo",
		Content:   viaduct.NewTemplate(templates, "templates/test.txt"),
		Variables: struct{ Name string }{Name: "Laura"},
	}.Create()
}
