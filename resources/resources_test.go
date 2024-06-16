package resources

import "github.com/surminus/viaduct"

var testLogger *viaduct.Logger

func init() {
	viaduct.Cli.SetSilent()
	testLogger = viaduct.NewLogger("Test", "Testing")
}
