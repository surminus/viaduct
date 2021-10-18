package viaduct

import flag "github.com/spf13/pflag"

// Configs sets different settings when running something with viaduct
type Configs struct {
	DryRun           bool
	OutputAttributes bool
}

// InitConfigs loads configuration
func InitConfigs(c *Configs) {
	var (
		dryRun           bool
		outputAttributes bool
	)

	flag.BoolVar(&dryRun, "dry-run", false, "Test changes with dry-run mode")
	flag.BoolVar(&outputAttributes, "output-attributes", false, "Output attributes only")
	flag.Parse()

	c.DryRun = dryRun
	c.OutputAttributes = outputAttributes
}
