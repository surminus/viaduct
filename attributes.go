package viaduct

import "os"

type Attributes struct {
	User string
}

func InitAttributes(a *Attributes) {
	a.User = os.Getenv("USER")
}
