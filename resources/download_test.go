package resources

import (
	"os"
	"testing"

	"github.com/h2non/gock"
	"github.com/stretchr/testify/assert"
	"github.com/surminus/viaduct"
)

func newTestDownload(t *testing.T, url, path string) *Download {
	d := &Download{
		Path: path,
		URL:  url,
	}

	err := d.PreflightChecks(testLogger)
	if err != nil {
		t.Fatal(err)
	}

	return d
}

func TestDown(t *testing.T) {
	t.Parallel()

	t.Run("basic", func(t *testing.T) {
		t.Parallel()
		defer gock.Off()

		testurl := "http://test.com"

		gock.New(testurl).
			Get("/").
			Reply(200).
			BodyString("OK")

		d := newTestDownload(t, testurl, "test/acceptance/download/basic.txt")

		err := d.Run(testLogger)
		assert.NoError(t, err)

		assert.Equal(t, true, viaduct.FileExists(d.Path))
		assert.Equal(t, true, viaduct.FileSize(d.Path) > 0)

		if err := os.RemoveAll(d.Path); err != nil {
			t.Fatal(err)
		}
	})
}
