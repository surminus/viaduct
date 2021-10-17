package main

import (
	"bytes"
	"embed"
	"fmt"
	"log"
	"text/template"

	"github.com/surminus/viaduct"
)

//go:embed templates
var templates embed.FS

type TmplData struct {
	Name string
}

func main() {
	testFile, err := templates.ReadFile("templates/test.txt")
	if err != nil {
		log.Fatal(err)
	}

	t, err := template.New("test").Parse(string(testFile))
	if err != nil {
		log.Fatal(err)
	}

	data := TmplData{Name: "Laura"}
	var b bytes.Buffer
	err = t.Execute(&b, &data)
	if err != nil {
		log.Fatal(err)
	}

	dir := viaduct.Directory{Path: "test"}.Create()
	viaduct.File{Path: fmt.Sprintf("%s/foo", dir.Path), Content: b.String()}.Create()
}
