package main

import (
	v "github.com/surminus/viaduct"
)

func main() {
	m := v.New()

	sleep := m.Add(v.E("sleep 5"))
	err := m.Add(v.E("touch blah/boo"))
	dir := m.Add(v.Dir("test"))

	m.Add(v.E("echo hello"), err)
	m.WithLock(sleep)

	m.Add(v.Pkg("cowsay"), dir)

	foo := m.Add(&v.File{
		Path:    "test/foo",
		Content: "bar",
	}, dir)

	m.Add(v.Pkg("cowsay"), sleep)

	deletefoo := m.Add(&v.File{Path: "test/foo", Delete: true}, foo)
	m.Add(&v.Directory{Path: "test", Delete: true}, dir, deletefoo)

	m.Run()
}
