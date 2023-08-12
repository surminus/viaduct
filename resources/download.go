package resources

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/user"
	"strconv"

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

	// Mode is the permissions set of the file
	Mode os.FileMode
	// Root enforces using the root user
	Root bool
	// User sets the user permissions by user name
	User string
	// Group sets the group permissions by group name
	Group string
	// UID sets the user permissions by UID
	UID int
	// GID sets the group permissions by GID
	GID int
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

	if a.Mode == 0 {
		a.Mode = 0o644
	}

	if a.User == "" && a.UID == 0 && !a.Root {
		if uid, err := strconv.Atoi(viaduct.Attribute.User.Uid); err != nil {
			return err
		} else {
			a.UID = uid
		}
	}

	if a.Group == "" && a.GID == 0 && !a.Root {
		if gid, err := strconv.Atoi(viaduct.Attribute.User.Gid); err != nil {
			return err
		} else {
			a.GID = gid
		}
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

	if viaduct.FileExists(path) && a.NotIfExists {
		log.Noop(logmsg)
		return nil
	}

	file, err := os.Create(a.Path)
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

	logmsg = fmt.Sprintf("%s -> %s (size: %s)", a.URL, path, humanize.Bytes(uint64(size)))
	log.Info(logmsg)

	uid := a.UID
	gid := a.GID

	if a.User != "" {
		u, err := user.Lookup(a.User)
		if err != nil {
			return err
		}

		uid, err = strconv.Atoi(u.Uid)
		if err != nil {
			return err
		}
	}

	if a.Group != "" {
		g, err := user.LookupGroup(a.Group)
		if err != nil {
			return err
		}

		gid, err = strconv.Atoi(g.Gid)
		if err != nil {
			return err
		}
	}

	chmodmsg := fmt.Sprintf("Permissions: %s -> %s", path, a.Mode)
	chownmsg := fmt.Sprintf("Permissions: %s -> %d:%d", path, uid, gid)

	if viaduct.MatchChown(path, uid, gid) {
		log.Noop(chownmsg)
	} else {
		if err := os.Chown(path, uid, gid); err != nil {
			return err
		}
		log.Info(chownmsg)
	}

	if viaduct.MatchChmod(path, a.Mode) {
		log.Noop(chmodmsg)
	} else {
		if err := os.Chown(path, uid, gid); err != nil {
			return err
		}
		log.Info(chownmsg)
	}

	return nil
}
