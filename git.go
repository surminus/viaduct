package viaduct

import (
	"log"
	"os"

	"gopkg.in/src-d/go-git.v4"
	"gopkg.in/src-d/go-git.v4/plumbing"
)

// Git manages a git repository
type Git struct {
	// Path specifies where to clone the repository to. Required.
	Path string
	// URL is the URL of the Git repository. Required.
	URL string

	// Reference specifies the reference to fetch. Defaults to "refs/heads/main".
	Reference string
	// Remote specifies the remote name. Defaults to "origin".
	RemoteName string
	// Ensure will continue to pull the latest changes. Optional.
	Ensure bool
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

	// Optional settings
	if g.Reference == "" {
		g.Reference = "refs/heads/main"
	}

	if g.RemoteName == "" {
		g.RemoteName = "origin"
	}
}

// Create creates or updates a file
func (g Git) Create() *Git {
	g.satisfy()

	log.Println("==> Git [create]", g.Path)
	if Config.DryRun {
		return &g
	}

	var pathExists bool
	if _, err := os.Stat(HelperExpandPath(g.Path)); err == nil {
		if !g.Ensure {
			return &g
		}

		pathExists = true
	}

	if pathExists {
		r, err := git.PlainOpen(HelperExpandPath(g.Path))
		if err != nil {
			log.Fatal(err)
		}

		w, err := r.Worktree()
		if err != nil {
			log.Fatal(err)
		}

		// nolint:exhaustivestruct
		err = w.Pull(&git.PullOptions{
			RemoteName:    "origin",
			Progress:      os.Stdout,
			ReferenceName: plumbing.ReferenceName(g.Reference),
		})
		if err != nil {
			if err != git.NoErrAlreadyUpToDate {
				log.Fatal(err)
			}
		}
	}

	if !pathExists {
		// nolint:exhaustivestruct
		_, err := git.PlainClone(HelperExpandPath(g.Path), false, &git.CloneOptions{
			Progress:      os.Stdout,
			ReferenceName: plumbing.ReferenceName(g.Reference),
			RemoteName:    "origin",
			URL:           g.URL,
		})
		if err != nil {
			log.Fatal(err)
		}
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

	if err := os.RemoveAll(HelperExpandPath(g.Path)); err != nil {
		log.Fatal(err)
	}

	return &g
}
