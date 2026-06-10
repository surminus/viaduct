package resources

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTemplatePreflightChecks(t *testing.T) {
	t.Run("requires source", func(t *testing.T) {
		tmpl := &Template{Dest: "/tmp/out"}

		err := tmpl.PreflightChecks(testLogger)
		assert.EqualError(t, err, "required parameter: Source")
	})

	t.Run("requires dest", func(t *testing.T) {
		tmpl := &Template{Source: "/tmp/in"}

		err := tmpl.PreflightChecks(testLogger)
		assert.EqualError(t, err, "required parameter: Dest")
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
			Dest:   filepath.Join(dir, "out"),
		}

		if err := tmpl.PreflightChecks(testLogger); err != nil {
			t.Fatal(err)
		}

		return tmpl
	}

	t.Run("renders variables", func(t *testing.T) {
		tmpl := newTestTemplate(t, "url: {{.slack_notifier_url}}\n")
		tmpl.Variables = map[string]string{"slack_notifier_url": "https://example.com"}

		err := tmpl.Run(testLogger)
		assert.NoError(t, err)

		content, err := os.ReadFile(tmpl.Dest)
		assert.NoError(t, err)
		assert.Equal(t, "url: https://example.com\n", string(content))
	})

	t.Run("missing variable errors", func(t *testing.T) {
		tmpl := newTestTemplate(t, "{{.missing}}")
		tmpl.Variables = map[string]string{}

		err := tmpl.Run(testLogger)
		assert.Error(t, err)
	})

	t.Run("creates missing parent dir", func(t *testing.T) {
		tmpl := newTestTemplate(t, "hello\n")
		// Point Dest at a subdirectory that does not exist yet.
		tmpl.Dest = filepath.Join(filepath.Dir(tmpl.Dest), "nested", "out")
		tmpl.CreateDirIfMissing = true

		err := tmpl.Run(testLogger)
		assert.NoError(t, err)

		content, err := os.ReadFile(tmpl.Dest)
		assert.NoError(t, err)
		assert.Equal(t, "hello\n", string(content))
	})

	t.Run("missing source errors", func(t *testing.T) {
		tmpl := newTestTemplate(t, "")
		tmpl.Source = filepath.Join(t.TempDir(), "nonexistent")

		err := tmpl.Run(testLogger)
		assert.Error(t, err)
	})
}
