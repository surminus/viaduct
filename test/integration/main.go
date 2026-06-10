package main

import (
	"os"
	"strings"

	"github.com/surminus/viaduct"
	"github.com/surminus/viaduct/resources"
)

// This configuration exercises viaduct resources inside a container, and is
// applied twice to check that runs are idempotent. The verify.sh script
// asserts the result afterwards. See run.sh for how it all fits together.
//
// The Service resource is not covered here because containers do not run
// systemd.

func main() {
	m := viaduct.New()

	// Sync package databases before installing anything
	var update *viaduct.Resource
	switch viaduct.Attribute.Platform.ID {
	case "debian", "ubuntu", "linuxmint":
		update = m.Add(resources.AptUpdate())
	case "arch", "manjaro":
		update = m.Add(resources.ExecLocked("pacman -Sy"))
	}

	if update != nil {
		m.Add(resources.Pkg("curl"), update)
	} else {
		m.Add(resources.Pkg("curl"))
	}

	// Users: a system user, a user with an explicit UID/GID, and adding
	// supplementary groups to an existing user
	m.Add(resources.SystemUser("testsvc"))
	m.Add(&resources.User{Name: "appuser", UID: 1500, GID: 1500, Shell: "/bin/sh"})
	m.Add(&resources.User{Name: "root", Groups: []string{"daemon"}})

	// Files, directories and links
	dir := m.Add(resources.Dir("/opt/viaduct-test"))
	file := m.Add(resources.CreateFile("/opt/viaduct-test/file", "hello\n"), dir)
	m.Add(resources.CreateLink("/opt/viaduct-test/link", "/opt/viaduct-test/file"), file)

	// Template rendering
	tmpl := m.Add(resources.CreateFile("/opt/viaduct-test/greeting.tmpl", "Hello, {{.name}}!\n"), dir)
	m.Add(&resources.Template{
		Source:    "/opt/viaduct-test/greeting.tmpl",
		Dest:      "/opt/viaduct-test/greeting",
		Variables: map[string]string{"name": "viaduct"},
	}, tmpl)

	// Archive extraction, from a tarball created on the fly
	tarball := m.Add(resources.Exec("tar -czf /opt/viaduct-test/bundle.tar.gz -C /opt/viaduct-test file"), file)
	m.Add(&resources.Archive{
		Path: "/opt/viaduct-test/bundle.tar.gz",
		Dest: "/opt/viaduct-test/extracted",
	}, tarball)

	// Execute with a lock, and with an Unless guard
	m.Add(resources.ExecLocked("touch /opt/viaduct-test/locked"), dir)
	m.Add(resources.ExecUnless("touch /opt/viaduct-test/should-not-exist", "true"), dir)

	// Sysctl: /proc/sys is read-only during a container build, so use the
	// current runtime value. This exercises writing the config file and
	// the noop path of the apply
	if swappiness, err := os.ReadFile("/proc/sys/vm/swappiness"); err == nil {
		m.Add(&resources.Sysctl{
			Name:   "99-viaduct-test",
			Values: map[string]string{"vm.swappiness": strings.TrimSpace(string(swappiness))},
		})
	}

	m.Run()
}
