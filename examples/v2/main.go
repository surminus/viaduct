package main

import (
	v "github.com/surminus/viaduct"
)

func main() {
	foo := v.New()

	test := foo.Run(v.Execute{Command: "echo hello!"})

	dir := foo.Create(v.Directory{Path: "test"})

	foo.Create(v.File{
		Path:    "test/foo",
		Content: "bar",
	}, v.DependsOn(dir), v.DependsOn(test))

	foo.Start()
}
