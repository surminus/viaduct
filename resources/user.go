package resources

import (
	"fmt"
	"os/user"
	"strconv"
	"strings"

	"github.com/surminus/viaduct"
)

// User creates a user. If the user already exists, only the supplementary
// Groups are applied.
type User struct {
	// Name is the name of the user
	Name string

	// UID is the user ID. Optional.
	UID int

	// GID is the group ID. If set, a group with the same name as the user
	// is created with this ID before the user is created. Optional.
	GID int

	// System creates a system user. Optional.
	System bool

	// Shell is the login shell. Defaults to /bin/bash. Optional.
	Shell string

	// Groups are supplementary groups the user should belong to. Optional.
	Groups []string
}

// SystemUser is a shortcut for declaring a system user with a
// non-interactive shell
func SystemUser(name string) *User {
	return &User{Name: name, System: true, Shell: "/bin/false"}
}

func (u *User) Description() string {
	return u.Name
}

func (u *User) Params() *viaduct.ResourceParams {
	// useradd and friends lock the passwd and group databases, so avoid
	// running alongside other resources that write them
	return viaduct.NewResourceParamsWithLockKey(viaduct.PasswdLock)
}

// PreflightChecks sets default values for the parameters for a particular
// resource
func (u *User) PreflightChecks(log *viaduct.Logger) error {
	// Set required values here, and error if they are not set
	if u.Name == "" {
		return fmt.Errorf("required parameter: Name")
	}

	// Set optional defaults here
	if u.Shell == "" {
		u.Shell = "/bin/bash"
	}

	if !viaduct.IsRoot() {
		return fmt.Errorf("user resource must be run as root")
	}

	return nil
}

func (u *User) OperationName() string {
	return "Create"
}

func (u *User) Run(log *viaduct.Logger) error {
	// The looked up user is passed on rather than looked up again, so a run
	// works from a single view of the passwd database
	if usr, ok := lookupUser(u.Name); ok {
		return u.update(log, usr)
	}

	return u.create(log)
}

// create creates the group (if a GID is given), the user, and assigns
// any supplementary groups
func (u *User) create(log *viaduct.Logger) error {
	if viaduct.Cli.DryRun {
		log.Info("created", "user", u.Name)
		return nil
	}

	if u.GID != 0 && !groupExists(u.Name) {
		if err := runCommand("groupadd", "-g", strconv.Itoa(u.GID), u.Name); err != nil {
			return fmt.Errorf("groupadd failed for %s: %w", u.Name, err)
		}

		log.Info("created", "group", u.Name, "gid", strconv.Itoa(u.GID))
	}

	args := []string{"useradd"}

	if u.System {
		args = append(args, "-r")
	}

	if u.UID != 0 {
		args = append(args, "-u", strconv.Itoa(u.UID))
	}

	if u.GID != 0 {
		args = append(args, "-g", strconv.Itoa(u.GID))
	}

	if len(u.Groups) > 0 {
		args = append(args, "-G", strings.Join(u.Groups, ","))
	}

	args = append(args, "-s", u.Shell, u.Name)

	if err := runCommand(args...); err != nil {
		return fmt.Errorf("useradd failed for %s: %w", u.Name, err)
	}

	log.Info("created", "user", u.Name)

	return nil
}

// update assigns supplementary groups to an existing user
func (u *User) update(log *viaduct.Logger, usr *user.User) error {
	if len(u.Groups) == 0 {
		log.Noop("exists", "user", u.Name)
		return nil
	}

	missing, err := u.missingGroups(usr)
	if err != nil {
		return err
	}

	if len(missing) == 0 {
		log.Noop("groups-unchanged", "user", u.Name)
		return nil
	}

	if viaduct.Cli.DryRun {
		log.Info("groups-added", "user", u.Name, "groups", strings.Join(missing, ","))
		return nil
	}

	if err := runCommand("usermod", "-a", "-G", strings.Join(missing, ","), u.Name); err != nil {
		return fmt.Errorf("usermod failed for %s: %w", u.Name, err)
	}

	log.Info("groups-added", "user", u.Name, "groups", strings.Join(missing, ","))

	return nil
}

// missingGroups returns the supplementary groups the user does not yet
// belong to
func (u *User) missingGroups(usr *user.User) ([]string, error) {
	gids, err := usr.GroupIds()
	if err != nil {
		return nil, err
	}

	current := make(map[string]bool)
	for _, gid := range gids {
		if g, err := user.LookupGroupId(gid); err == nil {
			current[g.Name] = true
		}
	}

	var missing []string
	for _, g := range u.Groups {
		if !current[g] {
			missing = append(missing, g)
		}
	}

	return missing, nil
}

// lookupUser returns the user and whether they exist
func lookupUser(name string) (*user.User, bool) {
	usr, err := user.Lookup(name)

	return usr, err == nil
}

// lookupGroup returns the group and whether it exists
func lookupGroup(name string) (*user.Group, bool) {
	grp, err := user.LookupGroup(name)

	return grp, err == nil
}

func groupExists(name string) bool {
	_, ok := lookupGroup(name)

	return ok
}
