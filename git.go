package viaduct

import (
	"log"
	"os"

	"gopkg.in/src-d/go-git.v4"
)

// Git manages a git repository
type Git struct {
	Path string
	URL  string
}

// satisfy sets default values for the parameters for a particular
// resource
func (g *Git) satisfy() {
	// Set required values here, and error if they are not set
	if g.Path == "" {
		log.Fatal("==> Git [error] Required parameter: Path")
	}

	if g.URL == "" {
		log.Fatal("==> Git [error] Required parameter: URL")
	}
}

// Create creates or updates a file
func (g Git) Create() *Git {
	g.satisfy()

	log.Println("==> Git [create]", g.Path)
	if Config.DryRun {
		return &g
	}

	if _, err := os.Stat(g.Path); err == nil {
		return &g
	}

	_, err := git.PlainClone(g.Path, false, &git.CloneOptions{
		URL:      g.URL,
		Progress: os.Stdout,
	})

	if err != nil {
		log.Fatal(err)
	}

	return &g
}

// Delete deletes a file
func (g Git) Delete() *Git {
	g.satisfy()

	log.Println("==> Git [delete]", g.Path)
	if Config.DryRun {
		return &g
	}

	err := os.RemoveAll(g.Path)
	if err != nil {
		log.Fatal(err)
	}

	return &g
}
