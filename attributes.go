package viaduct

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/user"
	"runtime"
	"strings"
	"sync"
)

const LinuxOsReleaseFile = "/etc/os-release"

// Attributes represents all possible attributes of a system
type SystemAttributes struct {
	User     user.User          `json:"user"`
	OS       string             `json:"os"`
	Arch     string             `json:"arch"`
	Hostname string             `json:"hostname"`
	Platform PlatformAttributes `json:"platform"`
	Custom   map[string]string  `json:"custom"`

	// runuser specifies the user that it's run as, rather
	// than the set user attribute, which can be modified
	runuser user.User
}

// PlatformAttributes has details about the platform (currently Linux only)
type PlatformAttributes struct {
	Name             string `json:"name"`
	Version          string `json:"version"`
	ID               string `json:"id"`
	IDLike           string `json:"idLike"`
	PrettyName       string `json:"prettyName"`
	VersionID        string `json:"versionId"`
	HomeURL          string `json:"homeUrl"`
	SupportURL       string `json:"supportUrl"`
	BugReportURL     string `json:"bugReportUrl"`
	PrivacyPolicyURL string `json:"privacyPolicyUrl"`
	VersionCodename  string `json:"versionCodename"`
	UbuntuCodename   string `json:"ubuntuCodename"`
}

// SetUser allows us to assign a default username
func (a *SystemAttributes) SetUser(username string) {
	NewLogger("Attribute", "Set").Info(fmt.Sprintf("User -> %s", username))

	u, err := user.Lookup(username)
	if err != nil {
		log.Fatal(err)
	}

	a.User = *u
}

var attributes struct {
	once sync.Once
	attr *SystemAttributes
	err  error
}

// LoadAttributes reads the system attributes, such as the current user,
// hostname and platform. They are read once and every call returns the same
// shared value. Anything that could not be read is left empty and reported in
// the error.
func LoadAttributes() (*SystemAttributes, error) {
	attributes.once.Do(func() {
		attributes.attr, attributes.err = loadAttributes(LinuxOsReleaseFile)
	})

	return attributes.attr, attributes.err
}

// Attributes returns the system attributes. Use LoadAttributes to see whether
// any could not be read.
func Attributes() *SystemAttributes {
	a, _ := LoadAttributes()
	return a
}

// loadAttributes reads the system attributes, carrying on past anything it
// cannot read.
func loadAttributes(releaseFile string) (*SystemAttributes, error) {
	a := &SystemAttributes{
		OS:     runtime.GOOS,
		Arch:   runtime.GOARCH,
		Custom: make(map[string]string),
	}

	var errs []error

	if u, err := user.Current(); err == nil {
		a.User = *u
		a.runuser = *u
	} else {
		errs = append(errs, fmt.Errorf("reading the current user: %w", err))
	}

	if h, err := os.Hostname(); err == nil {
		a.Hostname = h
	} else {
		errs = append(errs, fmt.Errorf("reading the hostname: %w", err))
	}

	if a.OS == "linux" {
		a.Platform = newPlatformAttributes(releaseFile)
	}

	return a, errors.Join(errs...)
}

// JSON returns a string representation of the loaded attributes
func (a *SystemAttributes) JSON() string {
	output, err := json.MarshalIndent(a, "", "    ")
	if err != nil {
		log.Fatal(err)
	}

	return string(output)
}

// AddCustom allows us to add custom attributes during a run
func (a *SystemAttributes) AddCustom(key, value string) {
	a.Custom[key] = value
}

// GetCustom returns the value of a custom attribute
func (a *SystemAttributes) GetCustom(key string) string {
	return a.Custom[key]
}

// nolint:cyclop
func newPlatformAttributes(releaseFile string) (p PlatformAttributes) {
	file, err := os.Open(releaseFile)
	if err != nil {
		return p
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		splitText := strings.Split(scanner.Text(), "=")
		// nolint:gomnd
		if len(splitText) < 2 {
			continue
		}

		switch splitText[0] {
		case "NAME":
			p.Name = strings.Trim(splitText[1], "\"")
		case "VERSION":
			p.Version = strings.Trim(splitText[1], "\"")
		case "ID":
			p.ID = strings.Trim(splitText[1], "\"")
		case "ID_LIKE":
			p.IDLike = strings.Trim(splitText[1], "\"")
		case "PRETTY_NAME":
			p.PrettyName = strings.Trim(splitText[1], "\"")
		case "VERSION_ID":
			p.VersionID = strings.Trim(splitText[1], "\"")
		case "HOME_URL":
			p.HomeURL = strings.Trim(splitText[1], "\"")
		case "SUPPORT_URL":
			p.SupportURL = strings.Trim(splitText[1], "\"")
		case "BUG_REPORT_URL":
			p.BugReportURL = strings.Trim(splitText[1], "\"")
		case "PRIVACY_POLICY_URL":
			p.PrivacyPolicyURL = strings.Trim(splitText[1], "\"")
		case "VERSION_CODENAME":
			p.VersionCodename = strings.Trim(splitText[1], "\"")
		case "UBUNTU_CODENAME":
			p.UbuntuCodename = strings.Trim(splitText[1], "\"")
		}
	}

	return p
}
