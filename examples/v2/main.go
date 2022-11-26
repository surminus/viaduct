package main

import (
	v "github.com/surminus/viaduct"
)

func main() {
	foo := v.New()

	foo.SetName(foo.Run(v.E("echo hello!")), "some-unique-name")
	dir := foo.Create(v.D("test"))

	cowsay := foo.Create(v.P("cowsay"), "some-unique-name", dir)

	foo.Create(v.File{
		Path:    "test/foo",
		Content: "bar",
	}, dir, "some-unique-name")

	foo.Delete(v.P("cowsay"), cowsay, "some-unique-name")

	foo.Start()
}
