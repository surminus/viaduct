package resources

import (
	"errors"
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

// dpkgQuery is the dpkg-query command line for checking packages
func dpkgQuery(names ...string) []string {
	return append([]string{"-W", "-f", "${Package}\t${binary:Package}\t${db:Status-Status}\n"}, names...)
}

// dpkgQueryLine is the command line dpkgQuery runs, as Runner.Commands has it
func dpkgQueryLine(names ...string) string {
	return viaduct.Command{Name: "dpkg-query", Args: dpkgQuery(names...)}.String()
}

func TestPackageRun(t *testing.T) {
	setPlatform(t, "ubuntu")

	t.Run("install", func(t *testing.T) {
		env := viaducttest.NewEnv(t)
		env.Runner.On("dpkg-query", dpkgQuery("curl", "git")...).Exit(1).Stderr("dpkg-query: no packages found matching curl\n")
		env.Runner.On("apt-get", "install", "-y", "curl", "git").Stdout("Reading package lists...\n").Stderr("warning\n")

		log, err := env.Apply(env.Context(), Pkgs("curl", "git"))
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Equal(t, []string{dpkgQueryLine("curl", "git"), "apt-get install -y curl git"}, env.Runner.Commands())

		// Without Verbose only stderr is logged, and never the query's
		assert.Equal(t, []string{"warning"}, env.Recorder.Messages(viaduct.LevelOutput))
	})

	t.Run("verbose logs stdout", func(t *testing.T) {
		env := viaducttest.NewEnv(t)
		env.Runner.On("dpkg-query", dpkgQuery("curl")...).Exit(1)
		env.Runner.On("apt-get", "install", "-y", "curl").Stdout("Reading package lists...\n")

		_, err := env.Apply(env.Context(), &Package{Names: []string{"curl"}, Verbose: true})
		require.NoError(t, err)

		assert.Equal(t, []string{"Reading package lists..."}, env.Recorder.Messages(viaduct.LevelOutput))
	})

	t.Run("purge", func(t *testing.T) {
		env := viaducttest.NewEnv(t)
		env.Runner.On("dpkg-query", dpkgQuery("nginx")...).Stdout("nginx\tnginx\tinstalled\n")
		env.Runner.On("apt-get", "purge", "-y", "nginx")

		log, err := env.Apply(env.Context(), PurgePkg("nginx"))
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Equal(t, []string{dpkgQueryLine("nginx"), "apt-get purge -y nginx"}, env.Runner.Commands())
	})

	t.Run("dry run install only queries", func(t *testing.T) {
		env := viaducttest.NewEnv(t)
		env.Runner.On("dpkg-query", dpkgQuery("curl")...).Exit(1)

		log, err := env.Apply(env.DryRunContext(), Pkg("curl"))
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Equal(t, []string{dpkgQueryLine("curl")}, env.Runner.Commands())
	})

	t.Run("dry run purge only queries", func(t *testing.T) {
		env := viaducttest.NewEnv(t)
		env.Runner.On("dpkg-query", dpkgQuery("nginx")...).Stdout("nginx\tnginx\tinstalled\n")

		log, err := env.Apply(env.DryRunContext(), PurgePkg("nginx"))
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Equal(t, []string{dpkgQueryLine("nginx")}, env.Runner.Commands())
	})

	t.Run("dry run reports installed packages as a noop", func(t *testing.T) {
		env := viaducttest.NewEnv(t)
		env.Runner.On("dpkg-query", dpkgQuery("curl")...).Stdout("curl\tcurl\tinstalled\n")

		log, err := env.Apply(env.DryRunContext(), Pkg("curl"))
		require.NoError(t, err)

		assert.False(t, log.Changed())
		assert.Equal(t, []string{dpkgQueryLine("curl")}, env.Runner.Commands())
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

func TestPackageDebian(t *testing.T) {
	setPlatform(t, "debian")

	t.Run("all installed is a noop", func(t *testing.T) {
		env := viaducttest.NewEnv(t)
		env.Runner.On("dpkg-query", dpkgQuery("curl", "libc6:arm64", "bash")...).
			Stdout("bash\tbash\tinstalled\ncurl\tcurl\tinstalled\nlibc6\tlibc6:arm64\tinstalled\n")

		log, err := env.Apply(env.Context(), Pkgs("curl", "libc6:arm64", "bash"))
		require.NoError(t, err)

		assert.False(t, log.Changed())
		assert.Equal(t, []string{"up-to-date"}, env.Recorder.Messages(viaduct.LevelNoop))
		assert.Equal(t, []string{dpkgQueryLine("curl", "libc6:arm64", "bash")}, env.Runner.Commands())
	})

	t.Run("installs only what is missing", func(t *testing.T) {
		env := viaducttest.NewEnv(t)
		// git was removed, leaving its configuration, and vim is unknown
		env.Runner.On("dpkg-query", dpkgQuery("curl", "git", "vim")...).
			Stdout("curl\tcurl\tinstalled\ngit\tgit\tconfig-files\n").
			Stderr("dpkg-query: no packages found matching vim\n").
			Exit(1)
		env.Runner.On("apt-get", "install", "-y", "git", "vim")

		log, err := env.Apply(env.Context(), Pkgs("curl", "git", "vim"))
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Equal(t, []string{dpkgQueryLine("curl", "git", "vim"), "apt-get install -y git vim"}, env.Runner.Commands())
		assert.Empty(t, env.Recorder.Messages(viaduct.LevelOutput))
	})

	t.Run("another architecture does not hide an installed one", func(t *testing.T) {
		env := viaducttest.NewEnv(t)
		env.Runner.On("dpkg-query", dpkgQuery("libc6")...).
			Stdout("libc6\tlibc6:arm64\tinstalled\nlibc6\tlibc6:armhf\tconfig-files\n")

		log, err := env.Apply(env.Context(), Pkg("libc6"))
		require.NoError(t, err)

		assert.False(t, log.Changed())
	})

	t.Run("a half-configured package is installed again", func(t *testing.T) {
		env := viaducttest.NewEnv(t)
		env.Runner.On("dpkg-query", dpkgQuery("curl")...).Stdout("curl\tcurl\thalf-configured\n")
		env.Runner.On("apt-get", "install", "-y", "curl")

		log, err := env.Apply(env.Context(), Pkg("curl"))
		require.NoError(t, err)

		assert.True(t, log.Changed())
	})

	t.Run("uninstall of absent packages is a noop", func(t *testing.T) {
		env := viaducttest.NewEnv(t)
		env.Runner.On("dpkg-query", dpkgQuery("nginx", "apache2")...).
			Stdout("nginx\tnginx\tconfig-files\napache2\tapache2\tnot-installed\n")

		log, err := env.Apply(env.Context(), &Package{Names: []string{"nginx", "apache2"}, Uninstall: true})
		require.NoError(t, err)

		assert.False(t, log.Changed())
		assert.Equal(t, []string{dpkgQueryLine("nginx", "apache2")}, env.Runner.Commands())
	})

	t.Run("uninstall removes only what is installed", func(t *testing.T) {
		env := viaducttest.NewEnv(t)
		env.Runner.On("dpkg-query", dpkgQuery("nginx", "apache2")...).
			Stdout("apache2\tapache2\tinstalled\n").
			Exit(1)
		env.Runner.On("apt-get", "remove", "-y", "apache2")

		log, err := env.Apply(env.Context(), &Package{Names: []string{"nginx", "apache2"}, Uninstall: true})
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Equal(t, []string{dpkgQueryLine("nginx", "apache2"), "apt-get remove -y apache2"}, env.Runner.Commands())
	})

	t.Run("purge takes a package left with its configuration", func(t *testing.T) {
		env := viaducttest.NewEnv(t)
		env.Runner.On("dpkg-query", dpkgQuery("cron", "hello")...).
			Stdout("cron\tcron\tconfig-files\nhello\thello\tnot-installed\n")
		env.Runner.On("apt-get", "purge", "-y", "cron")

		log, err := env.Apply(env.Context(), PurgePkgs("cron", "hello"))
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Equal(t, []string{dpkgQueryLine("cron", "hello"), "apt-get purge -y cron"}, env.Runner.Commands())
	})

	t.Run("purge of unknown packages is a noop", func(t *testing.T) {
		env := viaducttest.NewEnv(t)
		env.Runner.On("dpkg-query", dpkgQuery("hello")...).Exit(1)

		log, err := env.Apply(env.Context(), PurgePkg("hello"))
		require.NoError(t, err)

		assert.False(t, log.Changed())
	})

	t.Run("a versioned name is not queried", func(t *testing.T) {
		env := viaducttest.NewEnv(t)
		env.Runner.On("dpkg-query", dpkgQuery("git")...).Stdout("git\tgit\tnot-installed\n")
		env.Runner.On("apt-get", "remove", "-y", "curl=8.5.0")

		log, err := env.Apply(env.Context(), &Package{Names: []string{"curl=8.5.0", "git"}, Uninstall: true})
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Equal(t, []string{dpkgQueryLine("git"), "apt-get remove -y curl=8.5.0"}, env.Runner.Commands())
	})

	t.Run("a failed query fails the resource", func(t *testing.T) {
		env := viaducttest.NewEnv(t)
		env.Runner.On("dpkg-query", dpkgQuery("curl")...).Stderr("dpkg-query: error: database is locked\n").Exit(2)

		_, err := env.Apply(env.Context(), Pkg("curl"))
		assert.EqualError(t, err, "dpkg-query failed with exit status 2: dpkg-query: error: database is locked")
		assert.Equal(t, []string{dpkgQueryLine("curl")}, env.Runner.Commands())
	})

	t.Run("a query that cannot start fails the resource", func(t *testing.T) {
		env := viaducttest.NewEnv(t)
		env.Runner.On("dpkg-query", dpkgQuery("curl")...).Fail(errors.New("not found"))

		_, err := env.Apply(env.Context(), Pkg("curl"))
		assert.EqualError(t, err, "dpkg-query failed: not found")
	})
}

func TestPackageFedora(t *testing.T) {
	setPlatform(t, "fedora")

	t.Run("all installed is a noop", func(t *testing.T) {
		env := viaducttest.NewEnv(t)
		env.Runner.On("rpm", "-q", "curl", "git").Stdout("curl-8.9.1-4.fc41.aarch64\ngit-2.47.0-1.fc41.aarch64\n")

		log, err := env.Apply(env.Context(), Pkgs("curl", "git"))
		require.NoError(t, err)

		assert.False(t, log.Changed())
		assert.Equal(t, []string{"rpm -q curl git"}, env.Runner.Commands())
	})

	t.Run("installs only what is missing", func(t *testing.T) {
		env := viaducttest.NewEnv(t)
		env.Runner.On("rpm", "-q", "curl", "git", "vim").
			Stdout("curl-8.9.1-4.fc41.aarch64\npackage git is not installed\npackage vim is not installed\n").
			Exit(2)
		env.Runner.On("dnf", "install", "-y", "git", "vim")

		log, err := env.Apply(env.Context(), Pkgs("curl", "git", "vim"))
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Equal(t, []string{"rpm -q curl git vim", "dnf install -y git vim"}, env.Runner.Commands())
	})

	t.Run("uninstall of absent packages is a noop", func(t *testing.T) {
		env := viaducttest.NewEnv(t)
		env.Runner.On("rpm", "-q", "nginx").Stdout("package nginx is not installed\n").Exit(1)

		log, err := env.Apply(env.Context(), &Package{Names: []string{"nginx"}, Uninstall: true})
		require.NoError(t, err)

		assert.False(t, log.Changed())
		assert.Equal(t, []string{"rpm -q nginx"}, env.Runner.Commands())
	})

	t.Run("uninstall removes only what is installed", func(t *testing.T) {
		env := viaducttest.NewEnv(t)
		env.Runner.On("rpm", "-q", "nginx", "httpd").
			Stdout("package nginx is not installed\nhttpd-2.4.62-2.fc41.aarch64\n").
			Exit(1)
		env.Runner.On("dnf", "remove", "-y", "httpd")

		log, err := env.Apply(env.Context(), &Package{Names: []string{"nginx", "httpd"}, Uninstall: true})
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Equal(t, []string{"rpm -q nginx httpd", "dnf remove -y httpd"}, env.Runner.Commands())
	})

	t.Run("dry run only queries", func(t *testing.T) {
		env := viaducttest.NewEnv(t)
		env.Runner.On("rpm", "-q", "vim").Stdout("package vim is not installed\n").Exit(1)

		log, err := env.Apply(env.DryRunContext(), Pkg("vim"))
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Equal(t, []string{"rpm -q vim"}, env.Runner.Commands())
	})

	t.Run("a name given twice is counted twice", func(t *testing.T) {
		env := viaducttest.NewEnv(t)
		env.Runner.On("rpm", "-q", "vim", "vim").
			Stdout("package vim is not installed\npackage vim is not installed\n").
			Exit(2)
		env.Runner.On("dnf", "install", "-y", "vim", "vim")

		log, err := env.Apply(env.Context(), Pkgs("vim", "vim"))
		require.NoError(t, err)

		assert.True(t, log.Changed())
	})

	t.Run("an exit status the output does not explain fails", func(t *testing.T) {
		env := viaducttest.NewEnv(t)
		env.Runner.On("rpm", "-q", "curl").Stderr("error: rpmdb: damaged header\n").Exit(1)

		_, err := env.Apply(env.Context(), Pkg("curl"))
		assert.EqualError(t, err, "rpm -q failed with exit status 1: error: rpmdb: damaged header")
		assert.Equal(t, []string{"rpm -q curl"}, env.Runner.Commands())
	})
}

func TestPackageArch(t *testing.T) {
	setPlatform(t, "arch")

	t.Run("all installed is a noop", func(t *testing.T) {
		env := viaducttest.NewEnv(t)
		env.Runner.On("pacman", "-Q", "curl", "git").Stdout("curl 8.11.0-1\ngit 2.47.0-1\n")

		log, err := env.Apply(env.Context(), Pkgs("curl", "git"))
		require.NoError(t, err)

		assert.False(t, log.Changed())
		assert.Equal(t, []string{"pacman -Q curl git"}, env.Runner.Commands())
	})

	t.Run("installs only what is missing", func(t *testing.T) {
		env := viaducttest.NewEnv(t)
		env.Runner.On("pacman", "-Q", "curl", "vim").
			Stdout("curl 8.11.0-1\n").
			Stderr("error: package 'vim' was not found\n").
			Exit(1)
		env.Runner.On("pacman", "-S", "--noconfirm", "--needed", "vim")

		log, err := env.Apply(env.Context(), Pkgs("curl", "vim"))
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Equal(t, []string{"pacman -Q curl vim", "pacman -S --noconfirm --needed vim"}, env.Runner.Commands())
	})

	t.Run("a name provided by another package counts as installed", func(t *testing.T) {
		env := viaducttest.NewEnv(t)
		env.Runner.On("pacman", "-Q", "sh").Stdout("bash 5.2.037-1\n")

		log, err := env.Apply(env.Context(), Pkg("sh"))
		require.NoError(t, err)

		assert.False(t, log.Changed())
	})

	t.Run("uninstall of absent packages is a noop", func(t *testing.T) {
		env := viaducttest.NewEnv(t)
		env.Runner.On("pacman", "-Q", "nginx").Stderr("error: package 'nginx' was not found\n").Exit(1)

		log, err := env.Apply(env.Context(), &Package{Names: []string{"nginx"}, Uninstall: true})
		require.NoError(t, err)

		assert.False(t, log.Changed())
		assert.Equal(t, []string{"pacman -Q nginx"}, env.Runner.Commands())
	})

	t.Run("purge of absent packages is a noop", func(t *testing.T) {
		env := viaducttest.NewEnv(t)
		env.Runner.On("pacman", "-Q", "nginx").Stderr("error: package 'nginx' was not found\n").Exit(1)

		log, err := env.Apply(env.Context(), PurgePkg("nginx"))
		require.NoError(t, err)

		assert.False(t, log.Changed())
		assert.Equal(t, []string{"pacman -Q nginx"}, env.Runner.Commands())
	})

	t.Run("purge removes only what is installed", func(t *testing.T) {
		env := viaducttest.NewEnv(t)
		env.Runner.On("pacman", "-Q", "nginx", "apache").
			Stdout("apache 2.4.62-1\n").
			Stderr("error: package 'nginx' was not found\n").
			Exit(1)
		env.Runner.On("pacman", "-Rn", "--noconfirm", "apache")

		log, err := env.Apply(env.Context(), PurgePkgs("nginx", "apache"))
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Equal(t, []string{"pacman -Q nginx apache", "pacman -Rn --noconfirm apache"}, env.Runner.Commands())
	})

	t.Run("dry run only queries", func(t *testing.T) {
		env := viaducttest.NewEnv(t)
		env.Runner.On("pacman", "-Q", "nginx").Stdout("nginx 1.26.2-1\n")

		log, err := env.Apply(env.DryRunContext(), PurgePkg("nginx"))
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Equal(t, []string{"pacman -Q nginx"}, env.Runner.Commands())
	})

	t.Run("a failure without a missing package fails", func(t *testing.T) {
		env := viaducttest.NewEnv(t)
		env.Runner.On("pacman", "-Q", "curl").Stderr("error: could not open database\n").Exit(1)

		_, err := env.Apply(env.Context(), Pkg("curl"))
		assert.EqualError(t, err, "pacman -Q failed with exit status 1: error: could not open database")
	})
}

func TestPackageUnrecognisedPlatform(t *testing.T) {
	setPlatform(t, "plan9")

	env := viaducttest.NewEnv(t)

	_, err := env.Apply(env.Context(), Pkg("curl"))
	assert.EqualError(t, err, "unrecognised distribution: plan9")
	assert.Empty(t, env.Runner.Commands())
}
