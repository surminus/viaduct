package viaduct

import (
	"log"

	homedir "github.com/mitchellh/go-homedir"
)

// HelperExpandPath ensures that "~" are expanded
func HelperExpandPath(path string) string {
	p, err := homedir.Expand(path)
	if err != nil {
		log.Fatal(err)
	}

	return p
}
