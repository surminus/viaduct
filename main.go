package viaduct

import "log"

var Attribute Attributes

func init() {
	log.Println("Initialising attributes...")
	InitAttributes(&Attribute)
}
