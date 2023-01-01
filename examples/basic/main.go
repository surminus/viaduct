package main

import (
	"github.com/surminus/viaduct"
	"github.com/surminus/viaduct/resources"
)

func main() {
	m := viaduct.New()

	dir := m.Add(resources.Dir("/tmp/test"))
	m.Add(resources.Touch("/tmp/test/foo"), dir)

	m.Run()
}
