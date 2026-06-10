package viaduct

import (
	"log"
	"os"
	"strconv"

	flag "github.com/spf13/pflag"
)

// CliFlags configures the command-line toolset
type CliFlags struct {
	Attributes   bool
	DryRun       bool
	DumpManifest bool
	JSON         bool
	Quiet        bool
	Silent       bool
	Stdout       bool
}

// initCli loads command-line options
func initCli(c *CliFlags) {
	var (
		attributes   bool
		dryRun       bool
		dumpManifest bool
		jsonOutput   bool
		quiet        bool
		silent       bool
		stdout       bool
	)

	flag.BoolVar(&dryRun, "dry-run", false, "Test changes with dry-run mode")
	flag.BoolVar(&attributes, "attributes", false, "Display known attributes")
	flag.BoolVar(&dumpManifest, "dump-manifest", false, "Dump the full manifest after the run")
	flag.BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	flag.BoolVar(&quiet, "quiet", false, "Quiet mode will only display errors during a run")
	flag.BoolVar(&silent, "silent", false, "Silent mode will suppress all output")
	flag.BoolVar(&stdout, "stdout", envBool("VIADUCT_STDOUT"), "Log non-error output to STDOUT instead of STDERR (errors stay on STDERR)")
	flag.Parse()

	if silent && quiet {
		log.Fatal("Cannot use --silent and --quiet together")
	}

	c.Attributes = attributes
	c.DryRun = dryRun
	c.DumpManifest = dumpManifest
	c.JSON = jsonOutput
	c.Quiet = quiet
	c.Silent = silent
	c.Stdout = stdout
}

// envBool reads a boolean environment variable, returning false when it is
// unset or cannot be parsed.
func envBool(name string) bool {
	v, ok := os.LookupEnv(name)
	if !ok {
		return false
	}

	b, err := strconv.ParseBool(v)
	if err != nil {
		return false
	}

	return b
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

// SetJSON enables JSON output mode.
func (c *CliFlags) SetJSON() {
	c.JSON = true
}

// SetSilent enables silent mode.
func (c *CliFlags) SetSilent() {
	c.Silent = true
}

// SetStdout logs non-error output to STDOUT instead of STDERR.
func (c *CliFlags) SetStdout() {
	c.Stdout = true
}
