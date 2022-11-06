package viaduct

import (
	"bytes"
	"embed"
	"io/ioutil"
	"log"
	"os"
	"os/user"
	"strconv"
	"strings"
	"text/template"
	"time"
)

// File manages files on the filesystem
type File struct {
	// Path is the path of the file
	Path string
	// Content is the content of the file
	Content string
	// Mode is the permissions set of the file
	Mode os.FileMode
	// Sudo performs the action using sudo
	Sudo bool

	// User sets the user permissions by user name
	User string
	// Group sets the group permissions by group name
	Group string
	// UID sets the user permissions by UID
	UID int
	// GID sets the group permissions by GID
	GID int
}

// satisfy sets default values for the parameters for a particular
// resource
func (f *File) satisfy(log *logger) {
	// Set required values here, and error if they are not set
	if f.Path == "" {
		log.Fatal("Required parameter: Path")
	}

	// Set optional defaults here
	if f.Mode == 0 {
		f.Mode = 0o644
	}

	if f.User == "" && f.UID == 0 {
		if uid, err := strconv.Atoi(Attribute.User.Uid); err != nil {
			log.Fatal(err)
		} else {
			f.UID = uid
		}
	}

	if f.Group == "" && f.GID == 0 {
		if gid, err := strconv.Atoi(Attribute.User.Gid); err != nil {
			log.Fatal(err)
		} else {
			f.GID = gid
		}
	}
}

// EmbeddedFile is a small helper function to helper reading
// embedded files
func EmbeddedFile(files embed.FS, path string) string {
	out, err := files.ReadFile(path)
	if err != nil {
		log.Fatal(err)
	}

	return string(out)
}

// NewTemplate makes it easier to return the data required
// to parse a template
func NewTemplate(files embed.FS, path string, variables interface{}) string {
	out := EmbeddedFile(files, path)

	tmpl, err := template.New(time.Now().String()).Parse(out)
	if err != nil {
		log.Fatal(err)
	}

	var b bytes.Buffer
	err = tmpl.Execute(&b, variables)
	if err != nil {
		log.Fatal(err)
	}

	return b.String()
}

// Create creates or updates a file
func (f File) Create() *File {
	log := newLogger("File", "create")
	f.satisfy(log)

	log.Info(f.Path)
	if Config.DryRun {
		return &f
	}

	path := ExpandPath(f.Path)

	// If we want to run it as sudo, then we create a temporary file, write
	// the content, and then copy the file into place.
	if f.Sudo {
		tmp, err := os.CreateTemp(Attribute.TmpDir, "create-file-*")
		if err != nil {
			log.Fatal(err)
		}
		defer tmp.Close()

		err = os.WriteFile(tmp.Name(), []byte(f.Content), f.Mode)
		if err != nil {
			log.Fatal(err)
		}

		if err := SudoCommand("cp", tmp.Name(), path); err != nil {
			log.Fatal(err)
		}
	} else {
		err := ioutil.WriteFile(path, []byte(f.Content), f.Mode)
		if err != nil {
			log.Fatal(err)
		}
	}

	uid := f.UID
	gid := f.GID

	if f.User != "" {
		u, err := user.Lookup(f.User)
		if err != nil {
			log.Fatal(err)
		}

		uid, err = strconv.Atoi(u.Uid)
		if err != nil {
			log.Fatal(err)
		}
	}

	if f.Group != "" {
		g, err := user.LookupGroup(f.Group)
		if err != nil {
			log.Fatal(err)
		}

		gid, err = strconv.Atoi(g.Gid)
		if err != nil {
			log.Fatal(err)
		}
	}

	if f.Sudo {
		if err := SudoCommand("chown", strings.Join([]string{strconv.Itoa(uid), strconv.Itoa(gid)}, ":"), path); err != nil {
			log.Fatal(err)
		}
	} else {
		if err := os.Chown(path, uid, gid); err != nil {
			log.Fatal(err)
		}
	}

	return &f
}

// Delete deletes a file
func (f File) Delete() *File {
	log := newLogger("File", "delete")
	f.satisfy(log)

	if Config.DryRun {
		log.Info(f.Path)
		return &f
	}

	path := ExpandPath(f.Path)

	// If the file does not exist, return early
	if f.Sudo {
		if err := SudoCommand("test", "-f", path); err != nil {
			log.Noop(f.Path)
			return &f
		}

		if err := SudoCommand("rm", "-f", path); err != nil {
			log.Fatal(err)
		}
	} else {
		if !FileExists(path) {
			log.Noop(f.Path)
			return &f
		}

		if err := os.Remove(path); err != nil {
			log.Fatal(err)
		}
	}

	log.Info(f.Path)

	return &f
}
