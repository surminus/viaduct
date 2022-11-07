package viaduct

import (
	"fmt"
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
func (g *Git) satisfy(log *logger) {
	// Set required values here, and error if they are not set
	if g.Path == "" {
		log.Fatal("Required parameter: Path")
	}

	if g.URL == "" {
		log.Fatal("Required parameter: URL")
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
	log := newLogger("Git", "create")
	g.satisfy(log)

	path := ExpandPath(g.Path)
	logmsg := fmt.Sprintf("%s -> %s", g.URL, path)

	if Config.DryRun {
		log.Info(logmsg)
		return &g
	}

	var pathExists bool
	if FileExists(path) {
		if !g.Ensure {
			log.Noop(logmsg)
			return &g
		}

		pathExists = true
	}

	if pathExists {
		r, err := git.PlainOpen(ExpandPath(g.Path))
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
			if err == git.NoErrAlreadyUpToDate {
				log.Noop(logmsg)
			} else {
				log.Fatal(err)
			}
		} else {
			log.Info(logmsg)
		}
	}

	if !pathExists {
		// nolint:exhaustivestruct
		_, err := git.PlainClone(ExpandPath(g.Path), false, &git.CloneOptions{
			Progress:      os.Stdout,
			ReferenceName: plumbing.ReferenceName(g.Reference),
			RemoteName:    "origin",
			URL:           g.URL,
		})
		if err != nil {
			log.Fatal(err)
		}

		log.Info(logmsg)
	}

	return &g
}

// Delete deletes the git directory
func (g Git) Delete() *Git {
	log := newLogger("Git", "delete")
	g.satisfy(log)

	path := ExpandPath(g.Path)

	if Config.DryRun {
		log.Info(path)
		return &g
	}

	if DirExists(path) {
		if err := os.RemoveAll(ExpandPath(g.Path)); err != nil {
			log.Fatal(err)
		}

		log.Info(path)
	} else {
		log.Noop(path)
	}

	return &g
}
