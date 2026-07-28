package resources

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/surminus/viaduct"
)

func TestAptExecuteHelpers(t *testing.T) {
	assert.Equal(t, "apt-get dist-upgrade -q -y", DistUpgrade().Command)
	assert.Equal(t, "apt-get autoremove -q -y", AptAutoremove().Command)
	assert.Equal(t, "apt-mark hold docker containerd", AptHold("docker", "containerd").Command)
	assert.Equal(t, "dpkg -i /tmp/test.deb", InstallDeb("/tmp/test.deb").Command)

	for _, e := range []*Execute{DistUpgrade(), AptAutoremove(), AptHold("docker"), InstallDeb("/tmp/test.deb")} {
		assert.True(t, e.Params().GlobalLock)
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
