package main

import (
	"fmt"

	"github.com/surminus/viaduct"
)

func main() {
	viaduct.File{Path: "bar"}.Create()

	fmt.Println(viaduct.Attribute.User)
}
