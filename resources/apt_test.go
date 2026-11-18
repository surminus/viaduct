package resources

import (
	"io"
	"maps"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/surminus/viaduct"
	"github.com/surminus/viaduct/viaducttest"
)

func TestAptExecuteHelpers(t *testing.T) {
	assert.Equal(t, []string{"apt-get", "dist-upgrade", "-q", "-y"}, DistUpgrade().Args)
	assert.Equal(t, []string{"apt-get", "autoremove", "-q", "-y"}, AptAutoremove().Args)
	assert.Equal(t, []string{"apt-mark", "hold", "docker", "containerd"}, AptHold("docker", "containerd").Args)
	assert.Equal(t, []string{"dpkg", "-i", "/tmp/test.deb"}, InstallDeb("/tmp/test.deb").Args)

	// Arguments go to the program rather than to a shell, so values containing
	// spaces or metacharacters survive intact
	assert.Equal(t, []string{"dpkg", "-i", "/tmp/my pkg.deb"}, InstallDeb("/tmp/my pkg.deb").Args)
	assert.Equal(t, []string{"apt-mark", "hold", "foo; rm -rf /"}, AptHold("foo; rm -rf /").Args)

	for _, e := range []*Execute{DistUpgrade(), AptAutoremove(), AptHold("docker"), InstallDeb("/tmp/test.deb")} {
		assert.True(t, e.Params().GlobalLock)
		assert.Equal(t, viaduct.PackageLock, e.Params().LockKey)
		assert.Negative(t, e.Params().Timeout, "dpkg is not stopped part way through")

		// The helpers are valid resources in their own right
		assert.NoError(t, e.PreflightChecks(testCtx, testLogger))
	}
}

func TestInstallDebGuard(t *testing.T) {
	// Fake dpkg-deb and dpkg-query on PATH, so the guard runs through bash
	// as it does on a host
	bin := t.TempDir()
	writeScript := func(name, body string) {
		require.NoError(t, os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"+body), 0o755))
	}

	writeScript("dpkg-deb", `
[ "$1" = -f ] || exit 2
[ -f "$2" ] || exit 2
case "$3" in
Package) echo agent ;;
Version) echo 1.2.0 ;;
esac
`)

	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))

	deb := filepath.Join(t.TempDir(), "it's an agent.deb")
	require.NoError(t, os.WriteFile(deb, nil, 0o644))

	guard := func() bool {
		return unlessSucceeds(testCtx, nil, InstallDeb(deb).Unless, func(c viaduct.Command) viaduct.Command { return c })
	}

	writeScript("dpkg-query", `[ "$4" = agent ] && printf 'installed 1.2.0'`)
	assert.True(t, guard(), "the same version is installed")

	writeScript("dpkg-query", `[ "$4" = agent ] && printf 'installed 1.1.0'`)
	assert.False(t, guard(), "another version is installed")

	writeScript("dpkg-query", `echo "dpkg-query: no packages found matching $4" >&2; exit 1`)
	assert.False(t, guard(), "the package is not installed")
}

func TestPackageLockHasNoTimeout(t *testing.T) {
	assert.Negative(t, Pkg("curl").Params().Timeout)
	assert.Negative(t, (&Shell{Script: "apt-get install -y curl", LockKey: viaduct.PackageLock}).Params().Timeout)
	assert.Zero(t, Exec("true").Params().Timeout)
	assert.Zero(t, ExecLocked("true").Params().Timeout)
	assert.Zero(t, (&Shell{Script: "true", LockKey: viaduct.PasswdLock}).Params().Timeout)
}

func TestAptParams(t *testing.T) {
	t.Run("takes the package lock when updating", func(t *testing.T) {
		params := (&Apt{Name: "test", URI: "https://example.com", Update: true}).Params()

		assert.True(t, params.GlobalLock)
		assert.Equal(t, viaduct.PackageLock, params.LockKey)
	})

	t.Run("has no timeout when updating", func(t *testing.T) {
		assert.Negative(t, (&Apt{Name: "test", URI: "https://example.com", Update: true}).Params().Timeout)
		assert.Negative(t, AptUpdate().Params().Timeout)
		assert.Zero(t, (&Apt{Name: "test", URI: "https://example.com"}).Params().Timeout)
	})

	t.Run("takes no lock when only writing the sources file", func(t *testing.T) {
		params := (&Apt{Name: "test", URI: "https://example.com"}).Params()

		assert.False(t, params.GlobalLock)
		assert.Empty(t, params.LockKey)
	})
}

const (
	testAptList    = "/etc/apt/sources.list.d/example.list"
	testAptSources = "/etc/apt/sources.list.d/example.sources"
	testAptContent = "deb https://example.com/apt jammy main \n"
)

func newTestApt() *Apt {
	return &Apt{Name: "example", URI: "https://example.com/apt", Distribution: "jammy"}
}

// newAptEnv returns a test environment with the apt sources directory in it
func newAptEnv(t *testing.T) *viaducttest.Env {
	env := viaducttest.NewEnv(t)
	require.NoError(t, os.MkdirAll(env.Path("/etc/apt/sources.list.d"), 0o755))

	return env
}

func TestAptRun(t *testing.T) {
	t.Parallel()

	t.Run("create and update", func(t *testing.T) {
		t.Parallel()

		env := newAptEnv(t)
		env.Runner.On("apt-get", "update", "-y").Stdout("Hit:1 https://example.com/apt jammy InRelease\n")

		a := newTestApt()
		a.Update = true

		log, err := env.Apply(env.Context(), a)
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Equal(t, testAptContent, env.ReadFile(testAptList))
		assert.Equal(t, []string{"apt-get update -y"}, env.Runner.Commands())

		// apt-get update's stdout is not logged
		assert.Empty(t, env.Recorder.Messages(viaduct.LevelOutput))
	})

	t.Run("up to date is a noop, and does not update", func(t *testing.T) {
		t.Parallel()

		env := newAptEnv(t)
		env.WriteFile(testAptList, testAptContent)

		a := newTestApt()
		a.Update = true

		log, err := env.Apply(env.Context(), a)
		require.NoError(t, err)

		assert.False(t, log.Changed())
		assert.Empty(t, env.Runner.Commands())
	})

	t.Run("replaces the other format", func(t *testing.T) {
		t.Parallel()

		env := newAptEnv(t)
		env.WriteFile(testAptSources, "old")

		_, err := env.Apply(env.Context(), newTestApt())
		require.NoError(t, err)

		assert.Equal(t, testAptContent, env.ReadFile(testAptList))
		assert.False(t, viaduct.FileExists(env.Path(testAptSources)))
	})

	t.Run("sources format", func(t *testing.T) {
		t.Parallel()

		env := newAptEnv(t)

		a := newTestApt()
		a.Format = Sources
		a.PublicPgpKey = "-----BEGIN PGP PUBLIC KEY BLOCK-----\n\nkey\n-----END PGP PUBLIC KEY BLOCK-----\n"

		_, err := env.Apply(env.Context(), a)
		require.NoError(t, err)

		assert.Equal(t, "Types: deb\n"+
			"Signed-By:  -----BEGIN PGP PUBLIC KEY BLOCK-----\n .\n key\n -----END PGP PUBLIC KEY BLOCK-----\n"+
			"URIs: https://example.com/apt\nSuites: jammy\nComponents: main\n\n", env.ReadFile(testAptSources))
	})

	t.Run("fetches a signing key from a URL", func(t *testing.T) {
		t.Parallel()

		env := newAptEnv(t)
		require.NoError(t, os.MkdirAll(env.Path("/usr/share/keyrings"), 0o755))
		env.Runner.On("curl", "-sSfL", "--proto", "=https", "--proto-redir", "=https", "https://example.com/key.asc").Stdout("armoured")
		env.Runner.On("gpg", "--dearmor").Do(func(c viaduct.Command) error {
			in, err := io.ReadAll(c.Stdin)
			if err != nil {
				return err
			}

			_, err = io.WriteString(c.Stdout, "dearmoured "+string(in))

			return err
		})

		a := newTestApt()
		a.SigningKeyURL = "https://example.com/key.asc"

		log, err := env.Apply(env.Context(), a)
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Equal(t, []string{"curl -sSfL --proto =https --proto-redir =https https://example.com/key.asc", "gpg --dearmor"}, env.Runner.Commands())
		assert.Equal(t, "dearmoured armoured", env.ReadFile("/usr/share/keyrings/example.gpg"))

		// The sources file names the keyring where apt finds it, not under
		// the root
		assert.Equal(t,
			"deb [signed-by=/usr/share/keyrings/example.gpg] https://example.com/apt jammy main \n",
			env.ReadFile(testAptList),
		)
	})

	t.Run("dry run reports the change and writes nothing", func(t *testing.T) {
		t.Parallel()

		env := newAptEnv(t)

		a := newTestApt()
		a.Update = true
		a.SigningKeyURL = "https://example.com/key.asc"

		log, err := env.Apply(env.DryRunContext(), a)
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Equal(t, []string{"signing-key-fetched", "created", "updating"}, env.Recorder.Messages(viaduct.LevelChanged))
		assert.Empty(t, env.Runner.Commands())
		assert.False(t, viaduct.FileExists(env.Path(testAptList)))
	})

	t.Run("dry run reports up to date as a noop", func(t *testing.T) {
		t.Parallel()

		env := newAptEnv(t)
		env.WriteFile(testAptList, testAptContent)

		log, err := env.Apply(env.DryRunContext(), newTestApt())
		require.NoError(t, err)

		assert.False(t, log.Changed())
	})

	t.Run("delete", func(t *testing.T) {
		t.Parallel()

		env := newAptEnv(t)
		env.WriteFile(testAptList, testAptContent)
		env.Runner.On("apt-get", "update", "-y")

		a := newTestApt()
		a.Delete = true
		a.Update = true

		log, err := env.Apply(env.Context(), a)
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.False(t, viaduct.FileExists(env.Path(testAptList)))
		assert.Equal(t, []string{"apt-get update -y"}, env.Runner.Commands())
	})

	t.Run("delete is a noop when absent", func(t *testing.T) {
		t.Parallel()

		env := newAptEnv(t)

		a := newTestApt()
		a.Delete = true

		log, err := env.Apply(env.Context(), a)
		require.NoError(t, err)

		assert.False(t, log.Changed())
		assert.Empty(t, env.Runner.Commands())
	})

	t.Run("dry run delete leaves the file", func(t *testing.T) {
		t.Parallel()

		env := newAptEnv(t)
		env.WriteFile(testAptList, testAptContent)

		a := newTestApt()
		a.Delete = true

		log, err := env.Apply(env.DryRunContext(), a)
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Equal(t, testAptContent, env.ReadFile(testAptList))
	})

	t.Run("update only", func(t *testing.T) {
		t.Parallel()

		env := newAptEnv(t)
		env.Runner.On("apt-get", "update", "-y")

		log, err := env.Apply(env.Context(), AptUpdate())
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Equal(t, []string{"apt-get update -y"}, env.Runner.Commands())
	})

	t.Run("dry run update only runs nothing", func(t *testing.T) {
		t.Parallel()

		env := newAptEnv(t)

		log, err := env.Apply(env.DryRunContext(), AptUpdate())
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Empty(t, env.Runner.Commands())
	})
}

func TestAptParametersAreStable(t *testing.T) {
	t.Parallel()

	env := newAptEnv(t)
	params := map[string]string{"arch": "amd64", "trusted": "yes", "lang": "none", "pdiffs": "no", "by-hash": "yes"}

	apply := func() *viaduct.Logger {
		a := newTestApt()
		a.Parameters = maps.Clone(params)

		log, err := env.Apply(env.Context(), a)
		require.NoError(t, err)

		return log
	}

	assert.True(t, apply().Changed())
	assert.Equal(t, "deb [arch=amd64 by-hash=yes lang=none pdiffs=no trusted=yes] https://example.com/apt jammy main \n", env.ReadFile(testAptList))

	// Map iteration order is random, so repeat to catch unsorted output
	for range 20 {
		assert.False(t, apply().Changed())
	}
}

func TestAptSigningKeys(t *testing.T) {
	t.Parallel()

	keyring := "/usr/share/keyrings/example.gpg"

	newEnv := func(t *testing.T) *viaducttest.Env {
		env := newAptEnv(t)
		require.NoError(t, os.MkdirAll(env.Path("/usr/share/keyrings"), 0o755))

		return env
	}

	t.Run("a plain http URL is fetched without restricting the protocol", func(t *testing.T) {
		t.Parallel()

		env := newEnv(t)
		env.Runner.On("curl", "-sSfL", "http://example.com/key.asc").Stdout("armoured")
		env.Runner.On("gpg", "--dearmor").Stdout("key")

		a := newTestApt()
		a.SigningKeyURL = "http://example.com/key.asc"

		_, err := env.Apply(env.Context(), a)
		require.NoError(t, err)
		assert.Equal(t, "key", env.ReadFile(keyring))
	})

	t.Run("a failed dearmor writes no keyring", func(t *testing.T) {
		t.Parallel()

		env := newEnv(t)
		env.Runner.OnAny("curl").Stdout("<html>")
		env.Runner.On("gpg", "--dearmor").Stdout("partial").Exit(2)

		a := newTestApt()
		a.SigningKeyURL = "https://example.com/key.asc"

		_, err := env.Apply(env.Context(), a)
		require.Error(t, err)
		assert.NoFileExists(t, env.Path(keyring))
		assert.Empty(t, dirEntries(t, env, "/usr/share/keyrings"), "no temporary file is left behind")
	})

	t.Run("a keyserver key is received, exported and deleted", func(t *testing.T) {
		t.Parallel()

		env := newEnv(t)
		env.Runner.On("gpg", "--recv-keys", "--keyserver", "keyserver.ubuntu.com", "ABCD")
		env.Runner.On("gpg", "--export", "ABCD").Stdout("exported")
		env.Runner.On("gpg", "--delete-keys", "--yes", "ABCD")

		a := newTestApt()
		a.SigningKey = "ABCD"

		_, err := env.Apply(env.Context(), a)
		require.NoError(t, err)

		assert.Equal(t, []string{
			"gpg --recv-keys --keyserver keyserver.ubuntu.com ABCD",
			"gpg --export ABCD",
			"gpg --delete-keys --yes ABCD",
		}, env.Runner.Commands())
		assert.Equal(t, "exported", env.ReadFile(keyring))
		assert.Empty(t, env.Recorder.Messages(viaduct.LevelOutput), "the keyring is not logged")
		assert.Equal(t, "deb [signed-by="+keyring+"] https://example.com/apt jammy main \n", env.ReadFile(testAptList))
	})

	t.Run("the key is deleted from gpg even when the export fails", func(t *testing.T) {
		t.Parallel()

		env := newEnv(t)
		env.Runner.On("gpg", "--recv-keys", "--keyserver", "keyserver.ubuntu.com", "ABCD")
		env.Runner.On("gpg", "--export", "ABCD").Exit(2)
		env.Runner.On("gpg", "--delete-keys", "--yes", "ABCD")

		a := newTestApt()
		a.SigningKey = "ABCD"

		_, err := env.Apply(env.Context(), a)
		require.Error(t, err)

		assert.True(t, env.Runner.Ran("gpg", "--delete-keys", "--yes", "ABCD"))
		assert.NoFileExists(t, env.Path(keyring))
		assert.NoFileExists(t, env.Path(testAptList))
	})
}

func TestAptSourcesFormat(t *testing.T) {
	t.Parallel()

	t.Run("parameters, with arch as Architectures", func(t *testing.T) {
		t.Parallel()

		env := newAptEnv(t)

		apply := func() *viaduct.Logger {
			a := newTestApt()
			a.Format = Sources
			a.Parameters = map[string]string{"arch": "amd64", "Trusted": "yes", "Languages": "none", "PDiffs": "no"}

			log, err := env.Apply(env.Context(), a)
			require.NoError(t, err)

			return log
		}

		assert.True(t, apply().Changed())
		assert.Equal(t, "Types: deb\n"+
			"Languages: none\n"+
			"PDiffs: no\n"+
			"Trusted: yes\n"+
			"Architectures: amd64\n"+
			"URIs: https://example.com/apt\nSuites: jammy\nComponents: main\n\n", env.ReadFile(testAptSources))

		for range 20 {
			assert.False(t, apply().Changed())
		}
	})

	t.Run("a signing key URL is Signed-By", func(t *testing.T) {
		t.Parallel()

		env := newAptEnv(t)
		env.WriteFile("/usr/share/keyrings/example.gpg", "key")

		a := newTestApt()
		a.Format = Sources
		a.SigningKeyURL = "https://example.com/key.asc"

		_, err := env.Apply(env.Context(), a)
		require.NoError(t, err)

		assert.Equal(t, "Types: deb\nSigned-By: /usr/share/keyrings/example.gpg\n"+
			"URIs: https://example.com/apt\nSuites: jammy\nComponents: main\n\n", env.ReadFile(testAptSources))
		assert.Empty(t, env.Runner.Commands(), "an existing keyring is not fetched again")
	})
}

func TestAptUpdateFailureRestores(t *testing.T) {
	t.Parallel()

	t.Run("a new file is removed, and the next run updates again", func(t *testing.T) {
		t.Parallel()

		env := newAptEnv(t)
		env.Runner.On("apt-get", "update", "-y").Exit(100)

		a := newTestApt()
		a.Update = true

		_, err := env.Apply(env.Context(), a)
		require.Error(t, err)
		assert.NoFileExists(t, env.Path(testAptList))

		env.Runner.On("apt-get", "update", "-y")

		a = newTestApt()
		a.Update = true

		log, err := env.Apply(env.Context(), a)
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Equal(t, testAptContent, env.ReadFile(testAptList))
		assert.Equal(t, []string{"apt-get update -y", "apt-get update -y"}, env.Runner.Commands())
	})

	t.Run("a changed file and the other format are put back", func(t *testing.T) {
		t.Parallel()

		env := newAptEnv(t)
		env.WriteFile(testAptList, "old\n")
		env.WriteFile(testAptSources, "other\n")
		env.Runner.On("apt-get", "update", "-y").Exit(100)

		a := newTestApt()
		a.Update = true

		_, err := env.Apply(env.Context(), a)
		require.Error(t, err)

		assert.Equal(t, "old\n", env.ReadFile(testAptList))
		assert.Equal(t, "other\n", env.ReadFile(testAptSources))
	})

	t.Run("a deleted file is put back", func(t *testing.T) {
		t.Parallel()

		env := newAptEnv(t)
		env.WriteFile(testAptList, testAptContent)
		env.Runner.On("apt-get", "update", "-y").Exit(100)

		a := newTestApt()
		a.Delete = true
		a.Update = true

		_, err := env.Apply(env.Context(), a)
		require.Error(t, err)
		assert.Equal(t, testAptContent, env.ReadFile(testAptList))
	})
}
