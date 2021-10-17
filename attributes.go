package viaduct

import (
	"bufio"
	"encoding/json"
	"log"
	"os"
	"runtime"
	"strings"
)

// Attributes represents all possible attributes of a system
type Attributes struct {
	User     string `json:"user"`
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	Platform `json:"platform"`
}

// Platform has details about the platform (currently Linux only)
type Platform struct {
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
	a.User = os.Getenv("USER")
	a.OS = runtime.GOOS
	a.Arch = runtime.GOARCH

	if a.OS == "linux" {
		a.Platform = newPlatform()
	}
}

// JSON returns a string representation of the loaded attributes
func (a Attributes) JSON() string {
	output, err := json.MarshalIndent(a, "", "\t")
	if err != nil {
		log.Fatal(err)
	}

	return string(output)
}

func newPlatform() (p Platform) {
	file, err := os.Open("/etc/os-release")
	if err != nil {
		return p
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		splitText := strings.Split(scanner.Text(), "=")
		if len(splitText) < 2 {
			continue
		}

		switch splitText[0] {
		case "NAME":
			p.Name = splitText[1]
		case "VERSION":
			p.Version = splitText[1]
		case "ID":
			p.ID = splitText[1]
		case "ID_LIKE":
			p.IDLike = splitText[1]
		case "PRETTY_NAME":
			p.PrettyName = splitText[1]
		case "VERSION_ID":
			p.VersionID = splitText[1]
		case "HOME_URL":
			p.HomeURL = splitText[1]
		case "SUPPORT_URL":
			p.SupportURL = splitText[1]
		case "BUG_REPORT_URL":
			p.BugReportURL = splitText[1]
		case "PRIVACY_POLICY_URL":
			p.PrivacyPolicyURL = splitText[1]
		case "VERSION_CODENAME":
			p.VersionCodename = splitText[1]
		case "UBUNTU_CODENAME":
			p.UbuntuCodename = splitText[1]
		}
	}

	return p
}
