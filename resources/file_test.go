package resources

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/surminus/viaduct"
)

var testFilePath = "test/acceptance/file/test_file.txt"
var testFileContent = "Test Content"

var testFile = &File{
	Path:    testFilePath,
	Content: testFileContent,
}

var testLogger = viaduct.NewSilentLogger("Test", "Testing")

func TestFile(t *testing.T) {
	t.Parallel()

	err := testFile.PreflightChecks(testLogger)
	assert.NoError(t, err)

	t.Run("create", func(t *testing.T) {
		t.Parallel()

		err := testFile.Run(testLogger)
		assert.NoError(t, err)

		assert.Equal(t, true, viaduct.FileExists(testFilePath))
		assert.Equal(t, testFileContent, viaduct.FileContents(testFilePath))
	})

	t.Run("delete", func(t *testing.T) {
		t.Parallel()

		testFile.Delete = true
		testFile.Path = "test/acceptance/file/test_delete.txt"

		_, err := os.Create(testFile.Path)
		if err != nil {
			t.Fatal(err)
		}

		err = testFile.Run(testLogger)
		assert.NoError(t, err)

		assert.Equal(t, false, viaduct.FileExists(testFile.Path))
	})
}
