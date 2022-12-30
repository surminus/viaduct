package main

import (
	v "github.com/surminus/viaduct"
)

func main() {
	m := v.New()

	sleep := m.Add(v.E("sleep 5"))
	err := m.Add(v.E("touch blah/boo"))
	dir := m.Add(v.D("test"))

	m.Add(v.E("echo hello"), err)
	m.WithLock(sleep)

	m.Add(v.Pkg("cowsay"), dir)

	m.Add(v.File{
		Path:    "test/foo",
		Content: "bar",
	}, dir)

	m.Add(v.Pkg("cowsay"), sleep)

	m.Run()
}
