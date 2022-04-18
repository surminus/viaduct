package viaduct

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewPlatform(t *testing.T) {
	t.Parallel()

	actual := newPlatformAttributes("test/fixtures/os-release")
	expected := PlatformAttributes{
		Name:             "Linux Mint",
		Version:          "20.2 (Uma)",
		ID:               "linuxmint",
		IDLike:           "ubuntu",
		PrettyName:       "Linux Mint 20.2",
		VersionID:        "20.2",
		HomeURL:          "https://www.linuxmint.com/",
		SupportURL:       "https://forums.linuxmint.com/",
		BugReportURL:     "http://linuxmint-troubleshooting-guide.readthedocs.io/en/latest/",
		PrivacyPolicyURL: "https://www.linuxmint.com/",
		VersionCodename:  "uma",
		UbuntuCodename:   "focal",
	}

	assert.Equal(t, actual, expected)
}
