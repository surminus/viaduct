package main

import (
	v "github.com/surminus/viaduct"
)

func main() {
	foo := v.New()

	foo.Run(v.E("echo hello!"))
	dir := foo.Create(v.D("test"))

	foo.SetName(foo.Create(v.P("cowsay"), dir), "cowsay")

	foo.Create(v.File{
		Path:    "test/foo",
		Content: "bar",
	}, dir)

	foo.SetDep(foo.Delete(v.P("cowsay")), "cowsay")

	foo.Start()
}
