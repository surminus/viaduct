package viaduct

import (
	"bufio"
	"encoding/json"
	"log"
	"os"
	"os/user"
	"runtime"
	"strings"
)

// Attributes represents all possible attributes of a system
type Attributes struct {
	User     user.User          `json:"user"`
	OS       string             `json:"os"`
	Arch     string             `json:"arch"`
	Hostname string             `json:"hostname"`
	Platform PlatformAttributes `json:"platform"`
	Custom   map[string]string  `json:"custom"`
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

// InitAttributes populates the attributes
func InitAttributes(a *Attributes) {
	user, err := user.Current()
	if err != nil {
		log.Fatal(err)
	}

	a.User = *user
	a.OS = runtime.GOOS
	a.Arch = runtime.GOARCH

	a.Hostname, err = os.Hostname()
	if err != nil {
		log.Fatal(err)
	}

	if a.OS == "linux" {
		a.Platform = newPlatformAttributes("/etc/os-release")
	}

	a.Custom = make(map[string]string)
}

// JSON returns a string representation of the loaded attributes
func (a Attributes) JSON() string {
	output, err := json.MarshalIndent(a, "", "    ")
	if err != nil {
		log.Fatal(err)
	}

	return string(output)
}

// AddCustom allows us to add custom attributes during a run
func (a *Attributes) AddCustom(key, value string) {
	a.Custom[key] = value
}

// GetCustom returns the value of a custom attribute
func (a *Attributes) GetCustom(key string) string {
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
