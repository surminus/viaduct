package resources

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"

	humanize "github.com/dustin/go-humanize"
	"github.com/surminus/viaduct"
)

// Download will fetch data from the given URL, and write it to the given path.
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

	// Checksum is the expected SHA256 hex digest of the downloaded file.
	// When set, the download is verified after writing and fails on mismatch.
	Checksum string

	// Permissions manages permissions for the downloaded content
	Permissions
}

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

func (a *Download) PreflightChecks(log *viaduct.Logger) error {
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

func (a *Download) Run(log *viaduct.Logger) error {
	return a.get(log)
}

func (a *Download) get(log *viaduct.Logger) error {
	path := viaduct.ExpandPath(a.Path)

	if a.CreateDirIfMissing {
		if err := ensureParentDir(log, path); err != nil {
			return err
		}
	}

	if viaduct.Cli.DryRun {
		log.Info("downloaded", "url", a.URL, "path", path)
		return nil
	}

	if viaduct.FileExists(path) && a.NotIfExists {
		log.Noop("up-to-date", "url", a.URL, "path", path)
		return nil
	}

	file, err := os.Create(path)
	if err != nil {
		return err
	}

	var client http.Client
	resp, err := client.Get(a.URL)
	if err != nil {
		file.Close()
		os.Remove(path)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		file.Close()
		os.Remove(path)
		return fmt.Errorf("request received status code %d", resp.StatusCode)
	}

	size, err := io.Copy(file, resp.Body)
	if err != nil {
		file.Close()
		os.Remove(path)
		return err
	}

	// Close explicitly before chmod/chown rather than deferring: the file
	// must be closed so a downstream resource can exec it immediately (an
	// open write fd causes ETXTBSY), and closing here surfaces any delayed
	// write errors that can appear on network filesystems.
	if err := file.Close(); err != nil {
		return err
	}

	log.Info("downloaded", "url", a.URL, "path", path, "size", humanize.Bytes(uint64(size)))

	if a.Checksum != "" {
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		sum := sha256.Sum256(contents)
		if got := hex.EncodeToString(sum[:]); got != a.Checksum {
			os.Remove(path)
			return fmt.Errorf("checksum mismatch for %s: expected %s, got %s", path, a.Checksum, got)
		}
	}

	return a.setFilePermissions(
		log,
		path,
	)
}
