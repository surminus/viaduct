package viaduct

import flag "github.com/spf13/pflag"

// Configs sets different settings when running something with viaduct
type Configs struct {
	DryRun           bool
	DumpManifest     bool
	OutputAttributes bool
}

// initConfigs loads configuration
func initConfigs(c *Configs) {
	var (
		dryRun           bool
		dumpManifest     bool
		outputAttributes bool
	)

	flag.BoolVar(&dryRun, "dry-run", false, "Test changes with dry-run mode")
	flag.BoolVar(&outputAttributes, "output-attributes", false, "Output attributes only")
	flag.BoolVar(&dumpManifest, "dump-manifest", false, "dump the full manifest after the run")
	flag.Parse()

	c.DryRun = dryRun
	c.OutputAttributes = outputAttributes
	c.DumpManifest = dumpManifest
}
