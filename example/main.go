package main

import (
	"github.com/surminus/viaduct"
)

func main() {
	viaduct.File{Path: "bar"}.Create()
}
