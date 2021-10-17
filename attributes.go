package viaduct

import (
	"bufio"
	"os"
	"runtime"
	"strings"
)

// Attributes represents all possible attributes of a system
type Attributes struct {
	User string
	OS   string
	Arch string
	Platform
}

// Platform has details about the platform (currently Linux only)
type Platform struct {
	Name             string
	Version          string
	ID               string
	IDLike           string
	PrettyName       string
	VersionID        string
	HomeURL          string
	SupportURL       string
	BugReportURL     string
	PrivacyPolicyURL string
	VersionCodename  string
	UbuntuCodename   string
}

// InitAttributes populates the attributes
func InitAttributes(a *Attributes) {
	a.User = os.Getenv("USER")
	a.OS = runtime.GOOS
	a.Arch = runtime.GOARCH

	a.Platform = newPlatform()
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
