package resources

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTemplatePreflightChecks(t *testing.T) {
	t.Run("requires source", func(t *testing.T) {
		tmpl := &Template{Path: "/tmp/out"}

		err := tmpl.PreflightChecks(testCtx, testLogger)
		assert.EqualError(t, err, "required parameter: Source")
	})

	t.Run("requires dest", func(t *testing.T) {
		tmpl := &Template{Source: "/tmp/in"}

		err := tmpl.PreflightChecks(testCtx, testLogger)
		assert.EqualError(t, err, "required parameter: Path")
	})
}

func TestTemplate(t *testing.T) {
	newTestTemplate := func(t *testing.T, content string) *Template {
		dir := t.TempDir()
		source := filepath.Join(dir, "test.tmpl")

		err := os.WriteFile(source, []byte(content), 0o644)
		if err != nil {
			t.Fatal(err)
		}

		tmpl := &Template{
			Source: source,
			Path:   filepath.Join(dir, "out"),
		}

		if err := tmpl.PreflightChecks(testCtx, testLogger); err != nil {
			t.Fatal(err)
		}

		return tmpl
	}

	t.Run("renders variables", func(t *testing.T) {
		tmpl := newTestTemplate(t, "url: {{.slack_notifier_url}}\n")
		tmpl.Variables = map[string]string{"slack_notifier_url": "https://example.com"}

		err := tmpl.Run(testCtx, testLogger)
		assert.NoError(t, err)

		content, err := os.ReadFile(tmpl.Path)
		assert.NoError(t, err)
		assert.Equal(t, "url: https://example.com\n", string(content))
	})

	t.Run("missing variable errors", func(t *testing.T) {
		tmpl := newTestTemplate(t, "{{.missing}}")
		tmpl.Variables = map[string]string{}

		err := tmpl.Run(testCtx, testLogger)
		assert.Error(t, err)
	})

	t.Run("creates missing parent dir", func(t *testing.T) {
		tmpl := newTestTemplate(t, "hello\n")
		// Point Path at a subdirectory that does not exist yet.
		tmpl.Path = filepath.Join(filepath.Dir(tmpl.Path), "nested", "out")
		tmpl.CreateDirIfMissing = true

		err := tmpl.Run(testCtx, testLogger)
		assert.NoError(t, err)

		content, err := os.ReadFile(tmpl.Path)
		assert.NoError(t, err)
		assert.Equal(t, "hello\n", string(content))
	})

	t.Run("missing source errors", func(t *testing.T) {
		tmpl := newTestTemplate(t, "")
		tmpl.Source = filepath.Join(t.TempDir(), "nonexistent")

		err := tmpl.Run(testCtx, testLogger)
		assert.Error(t, err)
	})
}
