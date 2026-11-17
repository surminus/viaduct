package viaduct

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runOutput runs the manifest through runWith, returning the exit code and
// what was written to stdout and stderr
func runOutput(t *testing.T, ctx context.Context, m *Manifest, flags CliFlags) (int, string, string) {
	t.Helper()

	var stdout, stderr bytes.Buffer
	code := m.runWith(ctx, flags, &stdout, &stderr)

	return code, stdout.String(), stderr.String()
}

// runJSON runs the manifest with --json and decodes the document on stdout
func runJSON(t *testing.T, ctx context.Context, m *Manifest, flags CliFlags) (int, RunOutput) {
	t.Helper()

	flags.JSON = true
	code, stdout, _ := runOutput(t, ctx, m, flags)

	var out RunOutput
	require.NoError(t, json.Unmarshal([]byte(stdout), &out), stdout)

	return code, out
}

func failingRun(value string) *failingResource {
	return &failingResource{testResourceType: testResourceType{Value: value}, runErr: errors.New("boom")}
}

func failingPreflight(value string) *failingResource {
	return &failingResource{testResourceType: testResourceType{Value: value}, preflightErr: errors.New("missing Path")}
}

func TestRun(t *testing.T) {
	ctx := context.Background()

	t.Run("resources finishing while others are still starting", func(t *testing.T) {
		// Enough resources that the first ones finish before the last ones
		// have been started
		m := New()
		for i := range 1000 {
			m.Add(newTestResource(fmt.Sprintf("resource-%d", i)))
		}

		code, out := runJSON(t, ctx, m, CliFlags{})
		assert.Equal(t, 0, code)
		assert.Equal(t, "success", out.Status)
		require.Len(t, out.Resources, 1000)

		for _, r := range out.Resources {
			assert.Equal(t, "Success", r.Status, r.ResourceID)
		}
	})

	t.Run("exit codes", func(t *testing.T) {
		for name, tc := range map[string]struct {
			add  func(m *Manifest)
			code int
		}{
			"success": {
				add:  func(m *Manifest) { m.Add(newTestResource("a")) },
				code: 0,
			},
			"run failure": {
				add:  func(m *Manifest) { m.Add(failingRun("a")) },
				code: 1,
			},
			"preflight failure": {
				add:  func(m *Manifest) { m.Add(failingPreflight("a")) },
				code: 1,
			},
			"invalid manifest": {
				add: func(m *Manifest) {
					m.Add(newTestResource("a"))
					m.Add(newTestResource("a"))
				},
				code: 1,
			},
			"only an allowed failure failed": {
				add: func(m *Manifest) {
					m.AllowFailure(m.Add(failingRun("a")))
					m.Add(newTestResource("b"))
				},
				code: 0,
			},
			"something depends on an allowed failure": {
				add: func(m *Manifest) {
					a := m.Add(failingRun("a"))
					m.AllowFailure(a)
					m.Add(newTestResource("b"), a)
				},
				code: 1,
			},
		} {
			t.Run(name, func(t *testing.T) {
				m := New()
				tc.add(m)

				code, _, _ := runOutput(t, ctx, m, CliFlags{Silent: true})
				assert.Equal(t, tc.code, code)
			})
		}
	})

	t.Run("flag errors", func(t *testing.T) {
		var stderr bytes.Buffer

		var c CliFlags
		code, exit := flagsExit(parseFlags(&c, []string{"--dry-rnu"}), &stderr)
		assert.True(t, exit)
		assert.Equal(t, 2, code)
		assert.Contains(t, stderr.String(), "unknown flag: --dry-rnu")

		code, exit = flagsExit(parseFlags(&c, []string{"--help"}), &stderr)
		assert.True(t, exit)
		assert.Equal(t, 0, code)

		_, exit = flagsExit(parseFlags(&c, nil), &stderr)
		assert.False(t, exit)
	})

	t.Run("silent still reports the error", func(t *testing.T) {
		m := New()
		m.Add(failingRun("a"))

		code, stdout, stderr := runOutput(t, ctx, m, CliFlags{Silent: true})
		assert.Equal(t, 1, code)
		assert.Empty(t, stdout)
		assert.Equal(t, 1, strings.Count(stderr, "\n"), stderr)
		assert.Contains(t, stderr, "run failed")
	})

	t.Run("the failure tree names resources by description and ID", func(t *testing.T) {
		m := New()
		a := m.Add(failingRun("/etc/broken"))
		b := m.Add(newTestResource("/etc/after"), a)

		code, _, stderr := runOutput(t, ctx, m, CliFlags{})
		assert.Equal(t, 1, code)

		assert.Contains(t, stderr, fmt.Sprintf("failingResource [Test] /etc/broken (%s)", a.ResourceID))
		assert.Contains(t, stderr, fmt.Sprintf("└── testResourceType [Test] /etc/after (%s): upstream dependency failingResource /etc/broken (%s) failed", b.ResourceID, a.ResourceID))
		assert.Contains(t, stderr, "run with --dump-manifest to see every resource, or --json for the full report")
	})

	t.Run("preflight failures name the resource", func(t *testing.T) {
		m := New()
		a := m.Add(failingPreflight("/etc/broken"))

		code, _, stderr := runOutput(t, ctx, m, CliFlags{})
		assert.Equal(t, 1, code)
		assert.Contains(t, stderr, fmt.Sprintf("preflight-failed resource_id=%s resource_kind=failingResource description=/etc/broken", a.ResourceID))
	})
}

func TestRunJSON(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		m := New()
		a := m.Add(&changingResource{testResourceType{Value: "a"}})

		code, out := runJSON(t, ctx, m, CliFlags{})
		assert.Equal(t, 0, code)

		assert.Equal(t, "success", out.Status)
		assert.Empty(t, out.Error)
		assert.False(t, out.DryRun)
		assert.Empty(t, out.Failures)
		require.Len(t, out.Resources, 1)
		assert.Equal(t, string(a.ResourceID), out.Resources[0].ResourceID)
		assert.True(t, out.Resources[0].Changed)
	})

	t.Run("run failure", func(t *testing.T) {
		m := New()
		a := m.Add(failingRun("a"))
		b := m.Add(newTestResource("b"), a)

		code, out := runJSON(t, ctx, m, CliFlags{})
		assert.Equal(t, 1, code)

		assert.Equal(t, "failed", out.Status)
		assert.Contains(t, out.Error, "run failed")
		require.Len(t, out.Failures, 1)
		assert.Equal(t, string(a.ResourceID), out.Failures[0].ResourceID)
		assert.Equal(t, "boom", out.Failures[0].Error)
		require.Len(t, out.Failures[0].Dependents, 1)
		assert.Equal(t, string(b.ResourceID), out.Failures[0].Dependents[0].ResourceID)

		for _, r := range out.Resources {
			if r.ResourceID == string(b.ResourceID) {
				assert.Equal(t, string(a.ResourceID), r.Cause)
			}
		}
	})

	t.Run("preflight failure", func(t *testing.T) {
		m := New()
		a := m.Add(failingPreflight("a"))
		m.Add(newTestResource("b"))

		code, out := runJSON(t, ctx, m, CliFlags{})
		assert.Equal(t, 1, code)

		assert.Equal(t, "failed", out.Status)
		assert.Contains(t, out.Error, "preflight checks failed")
		require.Len(t, out.Failures, 1)
		assert.Equal(t, string(a.ResourceID), out.Failures[0].ResourceID)
		assert.Equal(t, "Preflight", out.Failures[0].Operation)
		assert.Equal(t, "missing Path", out.Failures[0].Error)
	})

	t.Run("invalid manifest", func(t *testing.T) {
		m := New()
		m.Add(newTestResource("a"))
		m.Add(newTestResource("a"))

		code, out := runJSON(t, ctx, m, CliFlags{})
		assert.Equal(t, 1, code)

		assert.Equal(t, "failed", out.Status)
		assert.Contains(t, out.Error, "is already in the manifest")
		assert.NotNil(t, out.Resources, "resources is a list even when empty")
	})
}

func TestRunDryRun(t *testing.T) {
	ctx := context.Background()

	newManifest := func() *Manifest {
		m := New()
		m.Add(&changingResource{testResourceType{Value: "a"}})

		return m
	}

	t.Run("text marks changes as dry", func(t *testing.T) {
		code, _, stderr := runOutput(t, ctx, newManifest(), CliFlags{DryRun: true})
		assert.Equal(t, 0, code)

		assert.Contains(t, stripColour(stderr), " DRY  changingResource [Test] changed value=a")
		assert.NotContains(t, stripColour(stderr), "  OK  changingResource", "a change that was not made is not OK")
		assert.Contains(t, stripColour(stderr), "WARN  Viaduct [Run]")
		assert.Contains(t, stderr, "dry-run")
	})

	t.Run("text marks real changes as OK", func(t *testing.T) {
		_, _, stderr := runOutput(t, ctx, newManifest(), CliFlags{})
		assert.Contains(t, stripColour(stderr), "  OK  changingResource [Test] changed value=a")
		assert.NotContains(t, stderr, "dry-run")
	})

	t.Run("JSON", func(t *testing.T) {
		code, out := runJSON(t, ctx, newManifest(), CliFlags{DryRun: true})
		assert.Equal(t, 0, code)
		assert.True(t, out.DryRun)
	})
}

func TestRunInterrupted(t *testing.T) {
	for sig, code := range map[syscall.Signal]int{
		syscall.SIGINT:  130,
		syscall.SIGTERM: 143,
	} {
		t.Run(sig.String(), func(t *testing.T) {
			ctx, cancel := context.WithCancelCause(context.Background())

			// a holds the run until it has been interrupted, so b never starts
			a := &startedResource{started: make(chan struct{})}
			b := newTestResource("b")

			m := New()
			m.Add(b, m.Add(a))

			go func() {
				<-a.started
				cancel(interrupt{sig: sig})
			}()

			got, out := runJSON(t, ctx, m, CliFlags{})
			assert.Equal(t, code, got)
			assert.Equal(t, "interrupted", out.Status)
			assert.Contains(t, out.Error, "interrupted by")
			assert.Empty(t, out.Failures, "an interrupted run is not a failure to explain")
			assert.False(t, b.ran.Load())
		})
	}

	t.Run("text", func(t *testing.T) {
		ctx, cancel := context.WithCancelCause(context.Background())
		cancel(interrupt{sig: syscall.SIGINT})

		m := New()
		m.Add(newTestResource("a"))

		code, _, stderr := runOutput(t, ctx, m, CliFlags{})
		assert.Equal(t, 130, code)
		assert.Contains(t, stderr, "interrupted")
		assert.NotContains(t, stderr, "Failed resources")
	})

	t.Run("a cancelled context without a signal", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		m := New()
		m.Add(newTestResource("a"))

		code, out := runJSON(t, ctx, m, CliFlags{})
		assert.Equal(t, 1, code)
		assert.Equal(t, "interrupted", out.Status)
	})
}

func TestDumpManifest(t *testing.T) {
	ctx := context.Background()

	t.Run("writes every resource to a private file", func(t *testing.T) {
		m := New()
		a := m.Add(newTestResource("a"))
		b := m.Add(failingRun("b"), a)

		code, out := runJSON(t, ctx, m, CliFlags{DumpManifest: true})
		assert.Equal(t, 1, code)
		require.NotEmpty(t, out.Manifest)
		t.Cleanup(func() { _ = os.Remove(out.Manifest) })

		info, err := os.Stat(out.Manifest)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

		content, err := os.ReadFile(out.Manifest)
		require.NoError(t, err)

		var dumped map[string]map[string]any
		require.NoError(t, json.Unmarshal(content, &dumped))

		require.Contains(t, dumped, string(a.ResourceID))
		require.Contains(t, dumped, string(b.ResourceID))
		assert.Equal(t, "Success", dumped[string(a.ResourceID)]["Status"])
		assert.Equal(t, "Failed", dumped[string(b.ResourceID)]["Status"])
		assert.Equal(t, "boom", dumped[string(b.ResourceID)]["Error"])
	})

	t.Run("the path is printed", func(t *testing.T) {
		m := New()
		m.Add(newTestResource("a"))

		_, _, stderr := runOutput(t, ctx, m, CliFlags{DumpManifest: true})

		_, after, found := strings.Cut(stderr, "manifest-written path=")
		require.True(t, found, stderr)

		path := strings.TrimSpace(strings.SplitN(after, "\n", 2)[0])
		t.Cleanup(func() { _ = os.Remove(path) })
		assert.FileExists(t, path)
	})

	t.Run("a resource that cannot be marshalled is a warning", func(t *testing.T) {
		m := New()
		m.Add(newTestResource("a"))
		m.Add(&funcResource{F: func() {}})

		code, _, stderr := runOutput(t, ctx, m, CliFlags{DumpManifest: true})
		assert.Equal(t, 0, code, "a failed dump does not fail the run")
		assert.Contains(t, stderr, "manifest-not-written")
	})
}

// startedResource says when it has started, then waits for its context to be
// cancelled
type startedResource struct {
	testResourceType
	started chan struct{}
}

func (s *startedResource) Run(ctx context.Context, log *Logger) error {
	close(s.started)
	<-ctx.Done()

	return ctx.Err()
}

// stripColour removes the colour codes from the text handler's tags
func stripColour(s string) string {
	for _, code := range []string{"\x1b[32m", "\x1b[36m", "\x1b[34;2m", "\x1b[33m", "\x1b[31m", "\x1b[0m"} {
		s = strings.ReplaceAll(s, code, "")
	}

	return s
}
