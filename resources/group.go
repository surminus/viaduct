package resources

import (
	"context"
	"fmt"
	"strconv"

	"github.com/surminus/viaduct"
)

// Group manages a group. To add a user to a group, use the Groups option on
// the User resource: this is for managing the group itself, such as creating
// one with a fixed GID before the users that belong to it.
type Group struct {
	// Name is the name of the group
	Name string

	// GID is the group ID. Optional.
	GID int

	// System creates a system group. Optional.
	System bool

	// Delete removes the group instead of creating it. Optional.
	Delete bool
}

// SystemGroup is a shortcut for declaring a system group
func SystemGroup(name string) *Group {
	return &Group{Name: name, System: true}
}

func (g *Group) Description() string {
	return g.Name
}

func (g *Group) Params() *viaduct.ResourceParams {
	// groupadd and groupdel lock the group database, so avoid running
	// alongside other resources that write it
	return viaduct.NewResourceParamsWithLockKey(viaduct.PasswdLock)
}

// PreflightChecks sets default values for the parameters for a particular
// resource
func (g *Group) PreflightChecks(ctx context.Context, log *viaduct.Logger) error {
	// Set required values here, and error if they are not set
	if g.Name == "" {
		return fmt.Errorf("required parameter: Name")
	}

	if err := requireRoot(ctx, "group"); err != nil {
		return err
	}

	// Set optional defaults here
	return nil
}

func (g *Group) OperationName() string {
	if g.Delete {
		return "Delete"
	}

	return "Create"
}

func (g *Group) Run(ctx context.Context, log *viaduct.Logger) error {
	if g.Delete {
		return g.delete(ctx, log)
	}

	return g.create(ctx, log)
}

func (g *Group) create(ctx context.Context, log *viaduct.Logger) error {
	// A group that already exists with a different GID is a conflict we
	// cannot resolve without renumbering everything that belongs to it, so
	// say so rather than silently leaving it alone
	if grp, ok := lookupGroup(g.Name); ok {
		if g.GID != 0 && grp.Gid != strconv.Itoa(g.GID) {
			return fmt.Errorf("group %s exists with gid %s, not %d", g.Name, grp.Gid, g.GID)
		}

		log.Noop("exists", "group", g.Name)
		return nil
	}

	args := []string{"groupadd"}

	if g.System {
		args = append(args, "-r")
	}

	if g.GID != 0 {
		args = append(args, "-g", strconv.Itoa(g.GID))
	}

	args = append(args, g.Name)

	if !change(ctx, log, "created", "group", g.Name) {
		return nil
	}

	if err := runCommand(ctx, log, args...); err != nil {
		return fmt.Errorf("groupadd failed for %s: %w", g.Name, err)
	}

	return nil
}

func (g *Group) delete(ctx context.Context, log *viaduct.Logger) error {
	if _, ok := lookupGroup(g.Name); !ok {
		log.Noop("up-to-date", "group", g.Name)
		return nil
	}

	if !change(ctx, log, "deleted", "group", g.Name) {
		return nil
	}

	if err := runCommand(ctx, log, "groupdel", g.Name); err != nil {
		return fmt.Errorf("groupdel failed for %s: %w", g.Name, err)
	}

	return nil
}
