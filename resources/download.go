package resources

import (
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
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("request received status code %d", resp.StatusCode)
	}

	size, err := io.Copy(file, resp.Body)
	if err != nil {
		return err
	}

	log.Info("downloaded", "url", a.URL, "path", path, "size", humanize.Bytes(uint64(size)))

	return a.setFilePermissions(
		log,
		path,
	)
}
