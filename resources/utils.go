package resources

import (
	"fmt"
	"os"
	"os/user"
	"strconv"

	"github.com/surminus/viaduct"
)

// Set permissions for a directory
func setDirectoryPermissions(
	log *viaduct.Logger,
	path string,
	uid, gid int,
	username, group string,
	mode os.FileMode,
	recursiveChown bool,
) error {
	if username != "" {
		u, err := user.Lookup(username)
		if err != nil {
			return err
		}

		uid, err = strconv.Atoi(u.Uid)
		if err != nil {
			return err
		}
	}

	if group != "" {
		g, err := user.LookupGroup(group)
		if err != nil {
			return err
		}

		gid, err = strconv.Atoi(g.Gid)
		if err != nil {
			return err
		}
	}

	chmodmsg := fmt.Sprintf("Permissions: %s -> %s", path, mode)
	chownmsg := fmt.Sprintf("Permissions: %s -> %d:%d", path, uid, gid)

	if viaduct.MatchChmod(path, mode) {
		log.Noop(chmodmsg)
	} else {
		err := os.Chmod(path, mode)
		if err != nil {
			return err
		}

		log.Info(chmodmsg)
	}

	// If it's a directory, recursively set ownership permissions
	if viaduct.IsDirectory(path) && recursiveChown {
		chownmsg += " (recursive)"
		var wasupdated bool

		if files, err := viaduct.ListFiles(path); err == nil {
			for _, f := range files {
				if viaduct.MatchChown(f, uid, gid) {
					continue
				}

				wasupdated = true
				if err := os.Chown(f, uid, gid); err != nil {
					return err
				}
			}
		} else {
			return err
		}

		if wasupdated {
			log.Info(chownmsg)
		} else {
			log.Noop(chownmsg)
		}
	} else {
		if viaduct.MatchChown(path, uid, gid) {
			log.Noop(chownmsg)
		} else {
			if err := os.Chown(path, uid, gid); err != nil {
				return err
			}

			log.Info(chownmsg)
		}
	}

	return nil
}

// Set permissions for a file
func setFilePermissions(
	log *viaduct.Logger,
	path string,
	uid, gid int,
	username, group string,
	mode os.FileMode,
) error {
	if username != "" {
		u, err := user.Lookup(username)
		if err != nil {
			return err
		}

		uid, err = strconv.Atoi(u.Uid)
		if err != nil {
			return err
		}
	}

	if group != "" {
		g, err := user.LookupGroup(group)
		if err != nil {
			return err
		}

		gid, err = strconv.Atoi(g.Gid)
		if err != nil {
			return err
		}
	}

	chmodmsg := fmt.Sprintf("Permissions: %s -> %s", path, mode)
	chownmsg := fmt.Sprintf("Permissions: %s -> %d:%d", path, uid, gid)

	if viaduct.MatchChown(path, uid, gid) {
		log.Noop(chownmsg)
	} else {
		if err := os.Chown(path, uid, gid); err != nil {
			return err
		}
		log.Info(chownmsg)
	}

	if viaduct.MatchChmod(path, mode) {
		log.Noop(chmodmsg)
	} else {
		if err := os.Chown(path, uid, gid); err != nil {
			return err
		}
		log.Info(chownmsg)
	}

	return nil
}
