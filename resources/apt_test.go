package resources

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/surminus/viaduct"
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

		// The helpers are valid resources in their own right
		assert.NoError(t, e.PreflightChecks(testLogger))
	}
}

func TestAptParams(t *testing.T) {
	t.Run("takes the package lock when updating", func(t *testing.T) {
		params := (&Apt{Name: "test", URI: "https://example.com", Update: true}).Params()

		assert.True(t, params.GlobalLock)
		assert.Equal(t, viaduct.PackageLock, params.LockKey)
	})

	t.Run("takes no lock when only writing the sources file", func(t *testing.T) {
		params := (&Apt{Name: "test", URI: "https://example.com"}).Params()

		assert.False(t, params.GlobalLock)
		assert.Empty(t, params.LockKey)
	})
}
