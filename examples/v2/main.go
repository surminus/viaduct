package main

import (
	v "github.com/surminus/viaduct"
)

func main() {
	foo := v.New()

	sleep := foo.Run(v.E("sleep 5"))
	err := foo.Run(v.E("touch blah/boo"))
	dir := foo.Create(v.D("test"))

	foo.Run(v.E("echo hello"), err)
	foo.WithLock(sleep)

	foo.Create(v.P("cowsay"), dir)

	foo.Create(v.File{
		Path:    "test/foo",
		Content: "bar",
	}, dir)

	foo.Delete(v.P("cowsay"), sleep)

	foo.Start()
}
