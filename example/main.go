package main

import (
	"fmt"

	"github.com/surminus/viaduct"
)

func main() {
	dir := viaduct.Directory{Path: "test"}.Create()
	viaduct.File{Path: fmt.Sprintf("%s/foo", dir.Path), Content: "bar"}.Create()

	fmt.Println(viaduct.Attribute.JSON())
}
