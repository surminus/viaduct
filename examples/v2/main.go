package main

import (
	v "github.com/surminus/viaduct"
)

func main() {
	foo := v.New()

	test := foo.Run(v.E("echo hello!"))
	dir := foo.Create(v.D("test"))

	cowsay := foo.Create(v.P("cowsay"), test, dir)

	foo.Create(v.File{
		Path:    "test/foo",
		Content: "bar",
	}, dir, test)

	foo.Delete(v.P("cowsay"), cowsay)

	foo.Start()
}
