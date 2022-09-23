package viaduct

import (
	"log"

	homedir "github.com/mitchellh/go-homedir"
)

// ExpandPath ensures that "~" are expanded.
func ExpandPath(path string) string {
	p, err := homedir.Expand(path)
	if err != nil {
		log.Fatal(err)
	}

	return p
}

// PrependSudo takes a slice of args and simply prepends sudo to the front.
func PrependSudo(args []string) []string {
	return append([]string{"sudo"}, args...)
}
