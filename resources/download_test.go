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

	t.Run("create with missing parent dir", func(t *testing.T) {
		// Not parallel: gock intercepts the global transport, so running
		// alongside the other gock-based subtest would clobber its mocks.
		defer gock.Off()

		testurl := "http://test-createp.com"

		gock.New(testurl).
			Get("/").
			Reply(200).
			BodyString("OK")

		dir := "test/acceptance/download/createp"
		if err := os.RemoveAll(dir); err != nil {
			t.Fatal(err)
		}

		d := &Download{
			URL:                testurl,
			Path:               dir + "/nested/basic.txt",
			CreateDirIfMissing: true,
		}
		if err := d.PreflightChecks(testLogger); err != nil {
			t.Fatal(err)
		}

		assert.Equal(t, false, viaduct.DirExists(dir))

		err := d.Run(testLogger)
		assert.NoError(t, err)

		assert.Equal(t, true, viaduct.FileExists(d.Path))

		if err := os.RemoveAll(dir); err != nil {
			t.Fatal(err)
		}
	})
}
