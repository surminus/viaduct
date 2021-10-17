package viaduct

import flag "github.com/spf13/pflag"

// Configs sets different settings when running something with viaduct
type Configs struct {
	DryRun bool
}

// InitConfigs loads configuration
func InitConfigs(c *Configs) {
	var dryRun bool

	flag.BoolVar(&dryRun, "dry-run", false, "Test changes with dry-run mode")
	flag.Parse()

	c.DryRun = dryRun
}
