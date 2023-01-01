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
}

func Wget(url, path string) *Download {
	return &Download{
		URL:  url,
		Path: path,
	}
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

	return nil
}

func (a *Download) OperationName() string {
	return "Get"
}

func (a *Download) Run(log *viaduct.Logger) error {
	return a.get(log)
}

func (a *Download) get(log *viaduct.Logger) error {
	path := viaduct.ExpandPath(a.Path)
	logmsg := fmt.Sprintf("%s -> %s", a.URL, path)

	if viaduct.Config.DryRun {
		log.Info(logmsg)
		return nil
	}

	file, err := os.Create(a.Path)
	if err != nil {
		return err
	}

	client := http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			req.URL.Opaque = a.URL
			return nil
		},
	}

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

	logmsg = fmt.Sprintf("%s -> %s (size: %s)", a.URL, path, humanize.Bytes(uint64(size)))
	log.Info(logmsg)

	return nil
}
