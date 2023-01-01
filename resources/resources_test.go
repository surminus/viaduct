package resources

import "github.com/surminus/viaduct"

var testLogger *viaduct.Logger

func init() {
	viaduct.Config.SetSilent()
	testLogger = viaduct.NewLogger("Test", "Testing")
}
