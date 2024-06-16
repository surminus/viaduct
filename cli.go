package viaduct

import (
	"log"

	flag "github.com/spf13/pflag"
)

// CliFlags configures the command-line toolset
type CliFlags struct {
	Attributes   bool
	DryRun       bool
	DumpManifest bool
	Quiet        bool
	Silent       bool
}

// initCli loads command-line options
func initCli(c *CliFlags) {
	var (
		attributes   bool
		dryRun       bool
		dumpManifest bool
		quiet        bool
		silent       bool
	)

	flag.BoolVar(&dryRun, "dry-run", false, "Test changes with dry-run mode")
	flag.BoolVar(&attributes, "attributes", false, "Display known attributes")
	flag.BoolVar(&dumpManifest, "dump-manifest", false, "Dump the full manifest after the run")
	flag.BoolVar(&quiet, "quiet", false, "Quiet mode will only display errors during a run")
	flag.BoolVar(&silent, "silent", false, "Silent mode will suppress all output")
	flag.Parse()

	if silent && quiet {
		log.Fatal("Cannot use --silent and --quiet together")
	}

	c.Attributes = attributes
	c.DryRun = dryRun
	c.DumpManifest = dumpManifest
	c.Quiet = quiet
	c.Silent = silent
}

// SetDryRun enables dry run mode.
func (c *CliFlags) SetDryRun() {
	c.DryRun = true
}

// SetDumpManifest enables dumping the manifest.
func (c *CliFlags) SetDumpManifest() {
	c.DumpManifest = true
}

// SetQuiet enables quiet mode.
func (c *CliFlags) SetQuiet() {
	c.Quiet = true
}

// SetSilent enables silent mode.
func (c *CliFlags) SetSilent() {
	c.Silent = true
}
