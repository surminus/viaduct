package viaduct

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsUbuntu(t *testing.T) {
	t.Run("ubuntu", func(t *testing.T) {
		Attribute.Platform = newPlatformAttributes("test/fixtures/os-release_ubuntu")
		assert.Equal(t, true, IsUbuntu())
		Attribute.Platform = newPlatformAttributes(LinuxOsReleaseFile)
	})

	t.Run("linuxmint", func(t *testing.T) {
		Attribute.Platform = newPlatformAttributes("test/fixtures/os-release_linuxmint")
		assert.Equal(t, true, IsUbuntu())
		Attribute.Platform = newPlatformAttributes(LinuxOsReleaseFile)
	})
}
