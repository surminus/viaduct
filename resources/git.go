package resources

import (
	"fmt"
	"os"

	"github.com/surminus/viaduct"
	"gopkg.in/src-d/go-git.v4"
	"gopkg.in/src-d/go-git.v4/plumbing"
)

// Git manages a Git repository
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
	// Delete will remove the Git directory.
	Delete bool

	// Permissions manages permissions for the repository
	Permissions
}

// Repo will add a new repository, and ensure that it stays up to date.
func Repo(path, url string) *Git {
	return &Git{Path: path, URL: url, Ensure: true}
}

func (g *Git) Params() *viaduct.ResourceParams {
	return viaduct.NewResourceParams()
}

// PreflightChecks sets default values for the parameters for a particular
// resource
func (g *Git) PreflightChecks(log *viaduct.Logger) error {
	// Set required values here, and error if they are not set
	if g.Path == "" {
		return fmt.Errorf("required parameter: Path")
	}

	if g.URL == "" {
		return fmt.Errorf("required parameter: URL")
	}

	// Optional settings
	if g.Reference == "" {
		g.Reference = "refs/heads/main"
	}

	if g.RemoteName == "" {
		g.RemoteName = "origin"
	}

	return g.preflightPermissions(pdir)
}

func (g *Git) OperationName() string {
	if g.Delete {
		return "Delete"
	}

	return "Create"
}

func (g *Git) Run(log *viaduct.Logger) error {
	if g.Delete {
		return g.deleteGit(log)
	} else {
		return g.createGit(log)
	}
}

func (g *Git) createGit(log *viaduct.Logger) error {
	path := viaduct.ExpandPath(g.Path)
	logmsg := fmt.Sprintf("%s -> %s", g.URL, path)

	if viaduct.Cli.DryRun {
		log.Info(logmsg)
		return nil
	}

	if viaduct.FileExists(path) && g.Ensure {
		r, err := git.PlainOpen(path)
		if err != nil {
			return err
		}

		w, err := r.Worktree()
		if err != nil {
			return err
		}

		if viaduct.Attribute.User.Username != "root" {
			err = os.Setenv("SSH_KNOWN_HOSTS", viaduct.ExpandPath("~/.ssh/known_hosts"))
			if err != nil {
				return err
			}
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
				return err
			}
		} else {
			log.Info(logmsg)
		}
	}

	if !viaduct.FileExists(path) {
		progress := os.Stdout

		if viaduct.Cli.Quiet || viaduct.Cli.Silent {
			devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0755)
			if err != nil {
				return err
			}

			progress = devnull
		}

		// nolint:exhaustivestruct
		_, err := git.PlainClone(path, false, &git.CloneOptions{
			Progress:      progress,
			ReferenceName: plumbing.ReferenceName(g.Reference),
			RemoteName:    "origin",
			URL:           g.URL,
		})
		if err != nil {
			return err
		}

		log.Info(logmsg)
	}

	return g.setDirectoryPermissions(
		log,
		path,
		true,
	)
}

func (g *Git) deleteGit(log *viaduct.Logger) error {
	path := viaduct.ExpandPath(g.Path)

	if viaduct.Cli.DryRun {
		log.Info(path)
		return nil
	}

	if viaduct.DirExists(path) {
		if err := os.RemoveAll(path); err != nil {
			return err
		}

		log.Info(path)
	} else {
		log.Noop(path)
	}

	return nil
}
