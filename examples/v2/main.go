package main

import (
	v "github.com/surminus/viaduct"
)

func main() {
	foo := v.New()

	cowsay := foo.Install(v.Package{Name: "cowsay"})

	test := foo.Run(v.Execute{Command: "echo hello!"})
	dir := foo.Create(v.Directory{Path: "test"})

	foo.Create(v.File{
		Path:    "test/foo",
		Content: "bar",
	}, dir, test)

	foo.Remove(v.Package{Name: "cowsay"}, cowsay)

	foo.Start()
}
