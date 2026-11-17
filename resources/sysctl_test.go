package resources

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/surminus/viaduct"
	"github.com/surminus/viaduct/viaducttest"
)

func TestSysctlPreflightChecks(t *testing.T) {
	t.Run("requires name", func(t *testing.T) {
		s := &Sysctl{Values: map[string]string{"vm.swappiness": "10"}}

		err := s.PreflightChecks(testCtx, testLogger)
		assert.EqualError(t, err, "required parameter: Name")
	})

	t.Run("requires values", func(t *testing.T) {
		s := &Sysctl{Name: "99-test"}

		err := s.PreflightChecks(testCtx, testLogger)
		assert.EqualError(t, err, "required parameter: Values")
	})

	t.Run("rejects path separators in name", func(t *testing.T) {
		s := &Sysctl{Name: "../sudoers", Values: map[string]string{"vm.swappiness": "10"}}

		err := s.PreflightChecks(testCtx, testLogger)
		assert.EqualError(t, err, "name must be a plain filename, not a path: ../sudoers")
	})

	t.Run("IgnoreMissing keys must be in Values", func(t *testing.T) {
		s := &Sysctl{Name: "99-test", Values: map[string]string{"vm.swappiness": "10"}, IgnoreMissing: []string{"net.other"}}

		err := s.PreflightChecks(testCtx, testLogger)
		assert.EqualError(t, err, "IgnoreMissing key net.other is not in Values")
	})
}

func TestSysctlContent(t *testing.T) {
	s := &Sysctl{
		Name: "99-test",
		Values: map[string]string{
			"vm.swappiness":    "10",
			"fs.file-max":      "65536",
			"vm.max_map_count": "262144",
		},
	}

	expected := "fs.file-max = 65536\nvm.max_map_count = 262144\nvm.swappiness = 10\n"
	assert.Equal(t, expected, s.content())
}

func TestSysctlContentIgnoreMissing(t *testing.T) {
	s := &Sysctl{
		Name: "99-test",
		Values: map[string]string{
			"vm.swappiness":                      "10",
			"net.bridge.bridge-nf-call-iptables": "1",
		},
		IgnoreMissing: []string{"net.bridge.bridge-nf-call-iptables"},
	}

	assert.Equal(t, "-net.bridge.bridge-nf-call-iptables = 1\nvm.swappiness = 10\n", s.content())
}

func TestSysctlRun(t *testing.T) {
	const file = "/etc/sysctl.d/99-test.conf"

	setup := func(t *testing.T, swappiness string) *viaducttest.Env {
		env := viaducttest.NewEnv(t)
		env.WriteFile("/proc/sys/vm/swappiness", swappiness+"\n")
		return env
	}

	sysctl := func() *Sysctl {
		return &Sysctl{Name: "99-test", Values: map[string]string{"vm.swappiness": "10"}}
	}

	t.Run("writes the file and applies only it", func(t *testing.T) {
		env := setup(t, "60")
		fakeSysctl(env, "-p", file)

		log, err := env.Apply(env.Context(), sysctl())
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Equal(t, "vm.swappiness = 10\n", env.ReadFile(file))
		assert.Equal(t, []string{"sysctl -p " + file}, env.Runner.Commands())
	})

	t.Run("written and applied is a noop", func(t *testing.T) {
		env := setup(t, "10")
		env.WriteFile(file, "vm.swappiness = 10\n")

		log, err := env.Apply(env.Context(), sysctl())
		require.NoError(t, err)

		assert.False(t, log.Changed())
		assert.Empty(t, env.Runner.Calls())
	})

	t.Run("applied values with a stale file only rewrite the file", func(t *testing.T) {
		env := setup(t, "10")
		env.WriteFile(file, "vm.swappiness = 60\n")

		log, err := env.Apply(env.Context(), sysctl())
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.Equal(t, "vm.swappiness = 10\n", env.ReadFile(file))
		assert.Empty(t, env.Runner.Calls())
	})

	t.Run("a missing key that may be missing is not reapplied every run", func(t *testing.T) {
		env := setup(t, "10")
		env.WriteFile("/etc/sysctl.d/99-test.conf", "-net.bridge.bridge-nf-call-iptables = 1\nvm.swappiness = 10\n")

		s := sysctl()
		s.Values["net.bridge.bridge-nf-call-iptables"] = "1"
		s.IgnoreMissing = []string{"net.bridge.bridge-nf-call-iptables"}

		log, err := env.Apply(env.Context(), s)
		require.NoError(t, err)

		assert.False(t, log.Changed())
		assert.Empty(t, env.Runner.Calls())
	})

	t.Run("IgnoreMissing applies with -e", func(t *testing.T) {
		env := setup(t, "60")
		fakeSysctl(env, "-e", "-p", file)

		s := sysctl()
		s.Values["net.bridge.bridge-nf-call-iptables"] = "1"
		s.IgnoreMissing = []string{"net.bridge.bridge-nf-call-iptables"}

		_, err := env.Apply(env.Context(), s)
		require.NoError(t, err)
		assert.Equal(t, []string{"sysctl -e -p " + file}, env.Runner.Commands())
	})

	t.Run("an ignored key the kernel has is still compared", func(t *testing.T) {
		env := setup(t, "10")
		env.WriteFile("/proc/sys/net/bridge/bridge-nf-call-iptables", "0\n")
		env.WriteFile(file, "-net.bridge.bridge-nf-call-iptables = 1\nvm.swappiness = 10\n")
		fakeSysctl(env, "-e", "-p", file)

		s := sysctl()
		s.Values["net.bridge.bridge-nf-call-iptables"] = "1"
		s.IgnoreMissing = []string{"net.bridge.bridge-nf-call-iptables"}

		log, err := env.Apply(env.Context(), s)
		require.NoError(t, err)
		assert.True(t, log.Changed())
	})

	t.Run("a missing key that is not ignored is an error before applying", func(t *testing.T) {
		env := setup(t, "10")

		s := sysctl()
		s.Values["net.viaduct.missing"] = "1"

		_, err := env.Apply(env.Context(), s)
		assert.EqualError(t, err, "sysctl key net.viaduct.missing does not exist: load the module that provides it before this resource runs, or list it in IgnoreMissing if it may be absent")
		assert.Empty(t, env.Runner.Calls())
		assert.NoFileExists(t, env.Path(file), "nothing is written")
	})

	t.Run("a value sysctl did not set is an error", func(t *testing.T) {
		env := setup(t, "60")
		env.Runner.On("sysctl", "-p", file)

		_, err := env.Apply(env.Context(), sysctl())
		assert.EqualError(t, err, "sysctl -p "+file+" did not set vm.swappiness")
	})

	t.Run("dry run", func(t *testing.T) {
		env := setup(t, "60")

		log, err := env.Apply(env.DryRunContext(), sysctl())
		require.NoError(t, err)

		assert.True(t, log.Changed())
		assert.NoFileExists(t, env.Path(file))
		assert.Empty(t, env.Runner.Calls())
	})

	t.Run("a failed apply is an error", func(t *testing.T) {
		env := setup(t, "60")
		env.Runner.On("sysctl", "-p", file).Exit(255)

		_, err := env.Apply(env.Context(), sysctl())
		code, ok := viaduct.ExitCode(err)
		assert.True(t, ok)
		assert.Equal(t, 255, code)
	})

	t.Run("the file is written atomically", func(t *testing.T) {
		env := setup(t, "10")
		env.WriteFile(file, "old\n")

		before, err := os.Stat(env.Path(file))
		require.NoError(t, err)

		_, err = env.Apply(env.Context(), sysctl())
		require.NoError(t, err)

		after, err := os.Stat(env.Path(file))
		require.NoError(t, err)
		assert.False(t, os.SameFile(before, after))
	})
}

// fakeSysctl answers sysctl with these arguments as "sysctl -p" does, setting
// each value in the file whose key the kernel has
func fakeSysctl(env *viaducttest.Env, args ...string) {
	file := args[len(args)-1]

	env.Runner.On("sysctl", args...).Do(func(viaduct.Command) error {
		for _, line := range strings.Split(env.ReadFile(file), "\n") {
			key, value, ok := strings.Cut(line, " = ")
			if !ok {
				continue
			}

			path := env.Path(procSysPath(strings.TrimPrefix(key, "-")))
			if !viaduct.FileExists(path) {
				continue
			}

			if err := os.WriteFile(path, []byte(value+"\n"), 0o644); err != nil {
				return err
			}
		}

		return nil
	})
}
