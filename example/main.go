package main

import (
	"fmt"

	"github.com/surminus/viaduct"
)

func main() {
	dir := viaduct.Directory{Path: "test"}.Create()
	viaduct.File{Path: fmt.Sprintf("%s/foo", dir.Path), Content: "bar"}.Create()

	g := viaduct.Git{
		Path: "/tmp/viaduct",
		URL:  "https://github.com/surminus/viaduct",
	}.Create()

	fmt.Println(viaduct.Attribute.JSON())

	g.Delete()
	dir.Delete()
}
