package main

import (
	"github.com/surminus/viaduct"
	"github.com/surminus/viaduct/resources"
)

func main() {
	m := viaduct.New()

	sleep := m.Add(resources.Exec("sleep 5"))
	err := m.Add(resources.Exec("touch blah/boo"))
	dir := m.Add(resources.Dir("test"))

	m.Add(resources.ExecUnless("echo hello", "true"), err)
	m.WithLock(sleep)

	m.Add(resources.Pkg("cowsay"), dir)

	foo := m.Add(&resources.File{
		Path:    "test/foo",
		Content: "bar",
	}, dir)

	m.Add(resources.Pkg("cowsay"), sleep)

	deletefoo := m.Add(&resources.File{Path: "test/foo", Delete: true}, foo)
	m.Add(&resources.Directory{Path: "test", Delete: true}, dir, deletefoo)

	m.Add(resources.Echo("test"))

	m.Add(&resources.Example{})

	m.Add(resources.Wget("https://icanhazip.com", viaduct.TmpFile("ip.txt")))

	m.Run()
}
