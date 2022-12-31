package viaduct

import (
	"fmt"
	"os"
	"strconv"

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
	// Mode is the permissions set of the directory
	Mode os.FileMode
	// Root enforces using the root user
	Root bool
	// User sets the user permissions by user name
	User string
	// Group sets the group permissions by group name
	Group string
	// UID sets the user permissions by UID
	UID int
	// GID sets the group permissions by GID
	GID int
	// Delete will remove the Git directory.
	Delete bool
}

func Repo(path, url string) *Git {
	return &Git{Path: path, URL: url}
}

func (g *Git) opts() *ResourceOptions {
	return NewResourceOptions()
}

// satisfy sets default values for the parameters for a particular
// resource
func (g *Git) satisfy(log *logger) error {
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

	if g.Mode == 0 {
		g.Mode = os.ModeDir | 0755
	} else {
		// Explicity set modedir to avoid diffs
		g.Mode = os.ModeDir | g.Mode
	}

	if g.User == "" && g.UID == 0 && !g.Root {
		if uid, err := strconv.Atoi(Attribute.User.Uid); err != nil {
			return err
		} else {
			g.UID = uid
		}
	}

	if g.Group == "" && g.GID == 0 && !g.Root {
		if gid, err := strconv.Atoi(Attribute.User.Gid); err != nil {
			return err
		} else {
			g.GID = gid
		}
	}

	return nil
}

func (g *Git) operationName() string {
	if g.Delete {
		return "Delete"
	}

	return "Create"
}

func (g *Git) run(log *logger) error {
	if g.Delete {
		return g.deleteGit(log)
	} else {
		return g.createGit(log)
	}
}

func (g *Git) createGit(log *logger) error {
	path := ExpandPath(g.Path)
	logmsg := fmt.Sprintf("%s -> %s", g.URL, path)

	if Config.DryRun {
		log.Info(logmsg)
		return nil
	}

	var pathExists bool
	if FileExists(path) {
		if !g.Ensure {
			log.Noop(logmsg)
			return nil
		}

		pathExists = true
	}

	if pathExists {
		r, err := git.PlainOpen(path)
		if err != nil {
			return err
		}

		w, err := r.Worktree()
		if err != nil {
			return err
		}

		if Attribute.User.Username != "root" {
			err = os.Setenv("SSH_KNOWN_HOSTS", ExpandPath("~/.ssh/known_hosts"))
			if err != nil {
				return err
			}
		}

		// nolint:exhaustivestruct
		err = w.Pull(&git.PullOptions{
			// Auth:          auth,
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

	if !pathExists {
		// nolint:exhaustivestruct
		_, err := git.PlainClone(path, false, &git.CloneOptions{
			Progress:      os.Stdout,
			ReferenceName: plumbing.ReferenceName(g.Reference),
			RemoteName:    "origin",
			URL:           g.URL,
		})
		if err != nil {
			return err
		}

		log.Info(logmsg)
	}

	return setDirectoryPermissions(
		newLogger("Git", "permissions"),
		path,
		g.UID, g.GID,
		g.User, g.Group,
		g.Mode,
	)
}

func (g *Git) deleteGit(log *logger) error {
	path := ExpandPath(g.Path)

	if Config.DryRun {
		log.Info(path)
		return nil
	}

	if DirExists(path) {
		if err := os.RemoveAll(path); err != nil {
			return err
		}

		log.Info(path)
	} else {
		log.Noop(path)
	}

	return nil
}
