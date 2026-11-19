package resources

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/surminus/viaduct"
)

// Generated IDs are used in --dump-manifest, --json and SetDep, so a field
// added to a resource must not change them. These are the IDs v0.8.0 gave.
func TestGeneratedIDsAreStable(t *testing.T) {
	t.Parallel()

	for want, r := range map[viaduct.ResourceID]viaduct.ResourceAttributes{
		"Apt_id-e7d02adc":       &Apt{Name: "x", URI: "http://x"},
		"Archive_id-31916ed7":   &Archive{Path: "/a.tar.gz", Dest: "/d"},
		"Directory_id-7425d988": &Directory{Path: "/d"},
		"Download_id-35412ab1":  &Download{URL: "http://x", Path: "/p"},
		"Execute_id-8b577969":   &Execute{Command: "true"},
		"File_id-a309288d":      &File{Path: "/f", Content: "c"},
		"Git_id-18b329b3":       &Git{URL: "http://x", Path: "/p"},
		"Group_id-b06d6687":     &Group{Name: "g"},
		"Line_id-81fc4940":      &Line{Path: "/f", Line: "l"},
		"Link_id-97c0a446":      &Link{Path: "/l", Source: "/s"},
		"Package_id-962297b8":   &Package{Names: []string{"curl"}},
		"Service_id-a9ff5061":   &Service{Name: "s", Action: ServiceRestart},
		"Shell_id-e9ecafd5":     &Shell{Script: "true"},
		"Sysctl_id-d071af8b":    &Sysctl{Name: "x"},
		"Template_id-96e7550b":  &Template{Source: "/s", Path: "/d"},
		"User_id-f02f2a58":      &User{Name: "u"},
	} {
		assert.Equal(t, want, viaduct.New().Add(r).ResourceID)
	}
}
