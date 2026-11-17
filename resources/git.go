package resources

import (
	"context"
	"fmt"
	"os"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/surminus/viaduct"
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

func (g *Git) Description() string {
	return fmt.Sprintf("%s -> %s", g.URL, g.Path)
}

func (g *Git) Params() *viaduct.ResourceParams {
	return viaduct.NewResourceParams()
}

// PreflightChecks sets default values for the parameters for a particular
// resource
func (g *Git) PreflightChecks(ctx context.Context, log *viaduct.Logger) error {
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

func (g *Git) Run(ctx context.Context, log *viaduct.Logger) error {
	if g.Delete {
		return g.deleteGit(ctx, log)
	}

	return g.createGit(ctx, log)
}

func (g *Git) createGit(ctx context.Context, log *viaduct.Logger) error {
	env := viaduct.EnvFrom(ctx)
	path := env.Path(viaduct.ExpandPath(g.Path))

	if viaduct.FileExists(path) && g.Ensure {
		if env.DryRun {
			// Whether a pull changes anything needs a fetch, so a dry run
			// reports a pull
			log.Info("pulled", "url", g.URL, "path", path)
		} else if err := g.pull(ctx, log, path); err != nil {
			return err
		}
	}

	if !viaduct.FileExists(path) {
		if env.DryRun {
			log.Info("cloned", "url", g.URL, "path", path)

			// Nothing to set permissions on yet
			return nil
		}

		progress := log.Writer(viaduct.Stdout)
		defer progress.Close()

		// nolint:exhaustivestruct
		_, err := git.PlainCloneContext(ctx, path, false, &git.CloneOptions{
			Progress:      progress,
			ReferenceName: plumbing.ReferenceName(g.Reference),
			RemoteName:    g.RemoteName,
			URL:           g.URL,
		})
		if err != nil {
			return err
		}

		log.Info("cloned", "url", g.URL, "path", path)
	}

	return g.setDirectoryPermissions(
		ctx,
		log,
		path,
		true,
	)
}

// pull brings the repository at path up to date with the remote
func (g *Git) pull(ctx context.Context, log *viaduct.Logger, path string) error {
	r, err := git.PlainOpen(path)
	if err != nil {
		return err
	}

	w, err := r.Worktree()
	if err != nil {
		return err
	}

	if viaduct.Attributes().User.Username != "root" {
		err = os.Setenv("SSH_KNOWN_HOSTS", viaduct.ExpandPath("~/.ssh/known_hosts"))
		if err != nil {
			return err
		}
	}

	progress := log.Writer(viaduct.Stdout)
	defer progress.Close()

	// nolint:exhaustivestruct
	err = w.PullContext(ctx, &git.PullOptions{
		RemoteName:    g.RemoteName,
		Progress:      progress,
		ReferenceName: plumbing.ReferenceName(g.Reference),
	})
	if err == git.NoErrAlreadyUpToDate {
		log.Noop("up-to-date", "url", g.URL, "path", path)
		return nil
	}

	if err != nil {
		return err
	}

	log.Info("pulled", "url", g.URL, "path", path)

	return nil
}

func (g *Git) deleteGit(ctx context.Context, log *viaduct.Logger) error {
	env := viaduct.EnvFrom(ctx)
	path := env.Path(viaduct.ExpandPath(g.Path))

	if !viaduct.DirExists(path) {
		log.Noop("up-to-date", "path", path)
		return nil
	}

	if !change(ctx, log, "deleted", "path", path) {
		return nil
	}

	return os.RemoveAll(path)
}
