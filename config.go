package viaduct

import (
	"log"

	flag "github.com/spf13/pflag"
)

// Configs sets different settings when running something with viaduct
type Configs struct {
	DryRun           bool
	DumpManifest     bool
	OutputAttributes bool
	Quiet            bool
	Silent           bool
}

// initConfigs loads configuration
func initConfigs(c *Configs) {
	var (
		dryRun           bool
		dumpManifest     bool
		outputAttributes bool
		quiet            bool
		silent           bool
	)

	flag.BoolVar(&dryRun, "dry-run", false, "Test changes with dry-run mode")
	flag.BoolVar(&outputAttributes, "output-attributes", false, "Output attributes only")
	flag.BoolVar(&dumpManifest, "dump-manifest", false, "Dump the full manifest after the run")
	flag.BoolVar(&quiet, "quiet", false, "Quiet mode will only display errors during a run")
	flag.BoolVar(&silent, "silent", false, "Silent mode will suppress all output")
	flag.Parse()

	if silent && quiet {
		log.Fatal("Cannot use --silent and --quiet together")
	}

	c.DryRun = dryRun
	c.DumpManifest = dumpManifest
	c.OutputAttributes = outputAttributes
	c.Quiet = quiet
	c.Silent = silent
}

// SetDryRun enables dry run mode.
func (c *Configs) SetDryRun() {
	c.DryRun = true
}

// SetDumpManifest enables dumping the manifest.
func (c *Configs) SetDumpManifest() {
	c.DumpManifest = true
}

// SetQuiet enables quiet mode.
func (c *Configs) SetQuiet() {
	c.Quiet = true
}

// SetSilent enables silent mode.
func (c *Configs) SetSilent() {
	c.Silent = true
}
