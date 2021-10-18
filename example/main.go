package main

import (
	"embed"
	"fmt"

	"github.com/surminus/viaduct"
)

//go:embed templates
var templates embed.FS

func main() {
	dir := viaduct.Directory{Path: "test"}.Create()

	viaduct.File{
		Path: fmt.Sprintf("%s/foo", dir.Path),
		Content: viaduct.NewTemplate(
			templates,
			"templates/test.txt",
			struct{ Name string }{Name: "Bella"}),
	}.Create()

	viaduct.Git{Path: "/tmp/viaduct", URL: "https://github.com/surminus/viaduct"}.Create()
}
