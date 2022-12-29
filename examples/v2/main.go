package main

import (
	v "github.com/surminus/viaduct"
)

func main() {
	m := v.New()

	sleep := m.Add(v.Run, v.E("sleep 5"))
	err := m.Add(v.Run, v.E("touch blah/boo"))
	dir := m.Add(v.Create, v.D("test"))

	m.Add(v.Run, v.E("echo hello"), err)
	m.WithLock(sleep)

	m.Add(v.Create, v.P("cowsay"), dir)

	m.Add(v.Create, v.File{
		Path:    "test/foo",
		Content: "bar",
	}, dir)

	m.Add(v.Delete, v.P("cowsay"), sleep)

	m.Run()
}
