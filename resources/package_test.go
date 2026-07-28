package resources

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Installing and uninstalling packages needs root and a real package manager,
// so the acceptance coverage lives in the Docker integration tests. What we can
// check here is the shortcuts, the operation naming and which platforms
// support what.

func TestPackageShortcuts(t *testing.T) {
	assert.Equal(t, []string{"curl"}, Pkg("curl").Names)
	assert.Equal(t, []string{"curl", "git"}, Pkgs("curl", "git").Names)

	assert.Equal(t, []string{"nginx"}, PurgePkg("nginx").Names)
	assert.True(t, PurgePkg("nginx").Purge)
	assert.Equal(t, []string{"nginx", "apache2"}, PurgePkgs("nginx", "apache2").Names)
	assert.True(t, PurgePkgs("nginx", "apache2").Purge)
}

func TestPackageOperationName(t *testing.T) {
	assert.Equal(t, "Install", Pkg("curl").OperationName())
	assert.Equal(t, "Uninstall", (&Package{Names: []string{"curl"}, Uninstall: true}).OperationName())
	assert.Equal(t, "Purge", PurgePkg("curl").OperationName())
}

func TestPurgeSupported(t *testing.T) {
	// Debian and Arch derivatives can drop a package's configuration
	for _, platform := range []string{"debian", "ubuntu", "linuxmint", "arch", "manjaro"} {
		assert.True(t, purgeSupported(platform), platform)
	}

	// dnf has no purge equivalent, so asking for one is rejected in preflight
	// rather than quietly doing a plain remove
	for _, platform := range []string{"fedora", "centos", "something-else"} {
		assert.False(t, purgeSupported(platform), platform)
	}
}

func TestInstallArgs(t *testing.T) {
	for _, platform := range []string{"debian", "ubuntu", "linuxmint"} {
		args, err := installArgs(platform, []string{"curl", "git"})
		assert.NoError(t, err)
		assert.Equal(t, []string{"apt-get", "install", "-y", "curl", "git"}, args, platform)
	}

	args, err := installArgs("fedora", []string{"curl"})
	assert.NoError(t, err)
	assert.Equal(t, []string{"dnf", "install", "-y", "curl"}, args)

	args, err = installArgs("arch", []string{"curl"})
	assert.NoError(t, err)
	assert.Equal(t, []string{"pacman", "-S", "--noconfirm", "--needed", "curl"}, args)

	_, err = installArgs("plan9", []string{"curl"})
	assert.EqualError(t, err, "unrecognised distribution: plan9")
}

func TestRemoveArgs(t *testing.T) {
	t.Run("remove leaves configuration alone", func(t *testing.T) {
		args, err := removeArgs("ubuntu", []string{"nginx"}, false)
		assert.NoError(t, err)
		assert.Equal(t, []string{"apt-get", "remove", "-y", "nginx"}, args)

		args, err = removeArgs("arch", []string{"nginx"}, false)
		assert.NoError(t, err)
		assert.Equal(t, []string{"pacman", "-R", "--noconfirm", "nginx"}, args)
	})

	t.Run("purge takes the configuration with it", func(t *testing.T) {
		args, err := removeArgs("ubuntu", []string{"nginx"}, true)
		assert.NoError(t, err)
		assert.Equal(t, []string{"apt-get", "purge", "-y", "nginx"}, args)

		args, err = removeArgs("arch", []string{"nginx"}, true)
		assert.NoError(t, err)
		assert.Equal(t, []string{"pacman", "-Rn", "--noconfirm", "nginx"}, args)
	})

	t.Run("dnf has no purge, and never gets asked for one", func(t *testing.T) {
		args, err := removeArgs("fedora", []string{"nginx"}, false)
		assert.NoError(t, err)
		assert.Equal(t, []string{"dnf", "remove", "-y", "nginx"}, args)
	})

	t.Run("unknown platform", func(t *testing.T) {
		_, err := removeArgs("plan9", []string{"nginx"}, false)
		assert.EqualError(t, err, "unrecognised distribution: plan9")
	})
}
