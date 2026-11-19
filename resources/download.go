package resources

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	humanize "github.com/dustin/go-humanize"
	"github.com/surminus/viaduct"
)

// Download will fetch data from the given URL, and write it to the given path.
// A download that starts on https is never redirected to anything else.
type Download struct {
	// URL is where to download the data from
	URL string
	// Path is where to store the downloaded data
	Path string

	// NotIfExists will not download the file if it already exists
	NotIfExists bool
	// CreateDirIfMissing creates the parent directory if it does not already
	// exist. The parent is created with 0755 and default ownership.
	CreateDirIfMissing bool

	// Checksum is the expected SHA256 hex digest of the file. A file that
	// already matches is not downloaded again, and a download that does not
	// match fails. Case does not matter.
	Checksum string

	// Permissions manages permissions for the downloaded content
	Permissions
}

// Wget downloads url to path
func Wget(url, path string) *Download {
	return &Download{
		URL:  url,
		Path: path,
	}
}

func (a *Download) Description() string {
	return fmt.Sprintf("%s -> %s", a.URL, a.Path)
}

func (a *Download) Params() *viaduct.ResourceParams {
	return viaduct.NewResourceParams()
}

func (a *Download) PreflightChecks(ctx context.Context, log *viaduct.Logger) error {
	if a.URL == "" {
		return fmt.Errorf("required parameter: URL")
	}

	if a.Path == "" {
		return fmt.Errorf("required parameter: Path")
	}

	return a.preflightPermissions(pfile)
}

func (a *Download) OperationName() string {
	return "Get"
}

func (a *Download) Run(ctx context.Context, log *viaduct.Logger) error {
	env := viaduct.EnvFrom(ctx)

	// Resolved once, so the download and the permissions go to the same file
	path, err := env.ResolveSymlink(viaduct.ExpandPath(a.Path))
	if err != nil {
		return err
	}

	if a.CreateDirIfMissing {
		if err := ensureParentDir(ctx, log, path); err != nil {
			return err
		}
	}

	exists := viaduct.FileExists(path)

	if exists && a.NotIfExists {
		log.Noop("up-to-date", "url", a.URL, "path", path)
		return nil
	}

	// A matching checksum means there is nothing to fetch
	if exists && a.Checksum != "" {
		got, err := sha256File(path)
		if err != nil {
			return err
		}

		if strings.EqualFold(got, a.Checksum) {
			log.Noop("up-to-date", "url", a.URL, "path", path)
			return a.setFilePermissions(ctx, log, path)
		}
	}

	if env.DryRun {
		// The size is unknown until the file is fetched
		log.Info("downloaded", "url", a.URL, "path", path)

		if !exists {
			// Nothing to set permissions on yet
			return nil
		}

		return a.setFilePermissions(ctx, log, path)
	}

	if err := a.download(ctx, path, log); err != nil {
		return err
	}

	return a.setFilePermissions(ctx, log, path)
}

// download streams the URL into path through WriteAtomic, hashing it on the
// way, so a failed or mismatched download leaves any existing file alone.
// Replacing the file rather than writing it also works on a running binary.
func (a *Download) download(ctx context.Context, path string, log *viaduct.Logger) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.URL, nil)
	if err != nil {
		return err
	}

	client := http.Client{CheckRedirect: checkRedirect}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("request received status code %d", resp.StatusCode)
	}

	var size int64

	err = viaduct.WriteAtomic(path, a.Mode, func(w io.Writer) error {
		hash := sha256.New()

		n, err := io.Copy(io.MultiWriter(w, hash), resp.Body)
		if err != nil {
			return err
		}

		size = n

		if got := hex.EncodeToString(hash.Sum(nil)); a.Checksum != "" && !strings.EqualFold(got, a.Checksum) {
			return fmt.Errorf("checksum mismatch for %s: expected %s, got %s", path, a.Checksum, got)
		}

		return nil
	})
	if err != nil {
		return err
	}

	log.Info("downloaded", "url", a.URL, "path", path, "size", humanize.Bytes(uint64(size)))

	return nil
}

// maxRedirects is how many redirects a download follows, as net/http does by
// default.
const maxRedirects = 10

// checkRedirect stops a download that started on https from being redirected
// to anything else, where the content could be changed in transit.
func checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return fmt.Errorf("stopped after %d redirects", maxRedirects)
	}

	for _, prev := range via {
		if prev.URL.Scheme == "https" && req.URL.Scheme != "https" {
			return fmt.Errorf("refusing a redirect from https to %s", req.URL.Redacted())
		}
	}

	return nil
}

// sha256File returns the hex SHA256 digest of a file
func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return "", err
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}
