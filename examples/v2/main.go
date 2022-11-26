package main

import (
	v "github.com/surminus/viaduct"
)

func main() {
	foo := v.New()

	foo.Run(v.E("echo hello!"))
	dir := foo.Create(v.D("test"))

	foo.Create(v.P("cowsay"), dir)

	foo.Create(v.File{
		Path:    "test/foo",
		Content: "bar",
	}, dir)

	foo.Delete(v.P("cowsay"))

	foo.Start()
}
