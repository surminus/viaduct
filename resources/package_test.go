package resources

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/surminus/viaduct"
	"github.com/surminus/viaduct/viaducttest"
)

// Installing and uninstalling packages for real needs root and a package
// manager, so the acceptance coverage lives in the Docker integration tests.
// The tests here fake the package manager's commands.

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

func TestHoldShortcuts(t *testing.T) {
	assert.True(t, HoldPkg("docker-ce").Hold)
	assert.Equal(t, []string{"docker-ce"}, HoldPkg("docker-ce").Names)
	assert.True(t, HoldPkgs("docker-ce", "containerd.io").Hold)

	assert.True(t, UnholdPkg("docker-ce").Unhold)
	assert.True(t, UnholdPkgs("docker-ce", "containerd.io").Unhold)

	assert.Equal(t, "Hold", HoldPkg("docker-ce").OperationName())
	assert.Equal(t, "Unhold", UnholdPkg("docker-ce").OperationName())
}

func TestHoldsToChange(t *testing.T) {
	held := map[string]bool{"curl": true, "git": true}

	t.Run("holding skips what is already held", func(t *testing.T) {
		assert.Equal(t, []string{"vim"}, holdsToChange([]string{"curl", "vim"}, held, true))
		assert.Empty(t, holdsToChange([]string{"curl", "git"}, held, true))
	})

	t.Run("unholding only touches what is held", func(t *testing.T) {
		assert.Equal(t, []string{"curl"}, holdsToChange([]string{"curl", "vim"}, held, false))
		assert.Empty(t, holdsToChange([]string{"vim"}, held, false))
	})

	t.Run("nothing held yet", func(t *testing.T) {
		assert.Equal(t, []string{"curl"}, holdsToChange([]string{"curl"}, map[string]bool{}, true))
		assert.Empty(t, holdsToChange([]string{"curl"}, map[string]bool{}, false))
	})
}

func TestHoldSupported(t *testing.T) {
	for _, platform := range []string{"debian", "ubuntu", "linuxmint"} {
		assert.True(t, holdSupported(platform), platform)
	}

	// dnf needs the versionlock plugin and pacman holds live in pacman.conf,
	// so asking for a hold there is rejected in preflight
	for _, platform := range []string{"fedora", "centos", "arch", "manjaro"} {
		assert.False(t, holdSupported(platform), platform)
	}
}

// setPlatform fakes the given platform until the test finishes
func setPlatform(t *testing.T, id string) {
	t.Helper()

	platform := &viaduct.Attributes().Platform
	saved := platform.ID
	platform.ID = id

	t.Cleanup(func() { platform.ID = saved })
}

func TestPackageRun(t *testing.T) {
	setPlatform(t, "ubuntu")

	t.Run("install", func(t *testing.T) {
		env := viaducttest.NewEnv(t)
		env.Runner.On("apt-get", "install", "-y", "curl", "git").Stdout("Reading package lists...\n").Stderr("warning\n")

		log, err := env.Apply(env.Context(), Pkgs("curl", "git"))
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Equal(t, []string{"apt-get install -y curl git"}, env.Runner.Commands())

		// Without Verbose only stderr is logged
		assert.Equal(t, []string{"warning"}, env.Recorder.Messages(viaduct.LevelOutput))
	})

	t.Run("verbose logs stdout", func(t *testing.T) {
		env := viaducttest.NewEnv(t)
		env.Runner.On("apt-get", "install", "-y", "curl").Stdout("Reading package lists...\n")

		_, err := env.Apply(env.Context(), &Package{Names: []string{"curl"}, Verbose: true})
		require.NoError(t, err)

		assert.Equal(t, []string{"Reading package lists..."}, env.Recorder.Messages(viaduct.LevelOutput))
	})

	t.Run("purge", func(t *testing.T) {
		env := viaducttest.NewEnv(t)
		env.Runner.On("apt-get", "purge", "-y", "nginx")

		log, err := env.Apply(env.Context(), PurgePkg("nginx"))
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Equal(t, []string{"apt-get purge -y nginx"}, env.Runner.Commands())
	})

	t.Run("dry run install runs nothing", func(t *testing.T) {
		env := viaducttest.NewEnv(t)

		log, err := env.Apply(env.DryRunContext(), Pkg("curl"))
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Empty(t, env.Runner.Commands())
	})

	t.Run("dry run purge runs nothing", func(t *testing.T) {
		env := viaducttest.NewEnv(t)

		log, err := env.Apply(env.DryRunContext(), PurgePkg("nginx"))
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Empty(t, env.Runner.Commands())
	})

	t.Run("hold only what is not held", func(t *testing.T) {
		env := viaducttest.NewEnv(t)
		env.Runner.On("apt-mark", "showhold").Stdout("curl\n")
		env.Runner.On("apt-mark", "hold", "git")

		log, err := env.Apply(env.Context(), HoldPkgs("curl", "git"))
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Equal(t, []string{"apt-mark showhold", "apt-mark hold git"}, env.Runner.Commands())
	})

	t.Run("hold is a noop once held", func(t *testing.T) {
		env := viaducttest.NewEnv(t)
		env.Runner.On("apt-mark", "showhold").Stdout("curl\ngit\n")

		log, err := env.Apply(env.Context(), HoldPkgs("curl", "git"))
		require.NoError(t, err)

		assert.False(t, log.Changed())
		assert.Equal(t, []string{"apt-mark showhold"}, env.Runner.Commands())
	})

	t.Run("unhold", func(t *testing.T) {
		env := viaducttest.NewEnv(t)
		env.Runner.On("apt-mark", "showhold").Stdout("curl\n")
		env.Runner.On("apt-mark", "unhold", "curl")

		log, err := env.Apply(env.Context(), UnholdPkgs("curl", "git"))
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Equal(t, []string{"apt-mark showhold", "apt-mark unhold curl"}, env.Runner.Commands())
	})

	t.Run("dry run hold checks but does not hold", func(t *testing.T) {
		env := viaducttest.NewEnv(t)
		env.Runner.On("apt-mark", "showhold")

		log, err := env.Apply(env.DryRunContext(), HoldPkg("curl"))
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Equal(t, []string{"apt-mark showhold"}, env.Runner.Commands())
	})

	t.Run("dry run hold reports a noop as a noop", func(t *testing.T) {
		env := viaducttest.NewEnv(t)
		env.Runner.On("apt-mark", "showhold").Stdout("curl\n")

		log, err := env.Apply(env.DryRunContext(), HoldPkg("curl"))
		require.NoError(t, err)

		assert.False(t, log.Changed())
		assert.Equal(t, []string{"apt-mark showhold"}, env.Runner.Commands())
	})

	t.Run("showhold failing fails the hold", func(t *testing.T) {
		env := viaducttest.NewEnv(t)
		env.Runner.On("apt-mark", "showhold").Exit(1)

		_, err := env.Apply(env.Context(), HoldPkg("curl"))
		assert.ErrorContains(t, err, "apt-mark showhold failed")
	})
}

func TestPackageUnrecognisedPlatform(t *testing.T) {
	setPlatform(t, "plan9")

	env := viaducttest.NewEnv(t)

	_, err := env.Apply(env.Context(), Pkg("curl"))
	assert.EqualError(t, err, "unrecognised distribution: plan9")
	assert.Empty(t, env.Runner.Commands())
}
