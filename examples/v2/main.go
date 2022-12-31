package main

import (
	v "github.com/surminus/viaduct"
)

func main() {
	m := v.New()

	sleep := m.Add(v.Exec("sleep 5"))
	err := m.Add(v.Exec("touch blah/boo"))
	dir := m.Add(v.Dir("test"))

	m.Add(v.ExecUnless("echo hello", "true"), err)
	m.WithLock(sleep)

	m.Add(v.Pkg("cowsay"), dir)

	foo := m.Add(&v.File{
		Path:    "test/foo",
		Content: "bar",
	}, dir)

	m.Add(v.Pkg("cowsay"), sleep)

	deletefoo := m.Add(&v.File{Path: "test/foo", Delete: true}, foo)
	m.Add(&v.Directory{Path: "test", Delete: true}, dir, deletefoo)

	m.Add(v.Echo("test"))

	m.Add(&v.Example{})

	m.Run()
}
