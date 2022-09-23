package main

import (
	v "github.com/surminus/viaduct"
)

func main() {
	pkg := v.Package{Name: "cowsay", Sudo: true}.Install()

	pkg.Remove()

	v.File{Path: "foo", Content: "bar", Sudo: true}.Create().Delete()
}
