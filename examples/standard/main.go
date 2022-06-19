package main

import (
	"embed"
	"fmt"

	v "github.com/surminus/viaduct"
)

//go:embed templates
var templates embed.FS

func main() {
	dir := v.Directory{Path: "test"}.Create()

	v.File{
		Path: fmt.Sprintf("%s/foo", dir.Path),
		Content: v.NewTemplate(
			templates,
			"templates/test.txt",
			struct{ Name string }{Name: "Bella"}),
	}.Create()

	link := v.Link{Path: "test/linked_file", Source: "test/foo"}.Create()
	link.Delete()

	v.Attribute.AddCustom("foo", "bar")

	fmt.Println(v.Attribute.GetCustom("foo"))

	v.Git{
		Path:   "~/tmp/viaduct",
		URL:    "https://github.com/surminus/viaduct",
		Ensure: true,
	}.Create()

	v.Execute{Command: "echo viaduct rocks!", Unless: "false"}.Run()
	v.Execute{Command: "echo viaduct rocks!", Unless: "true"}.Run()
}
