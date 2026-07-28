package viaduct

import (
	"log"
	"os"
	"strconv"
	"time"

	flag "github.com/spf13/pflag"
)

// CliFlags configures the command-line toolset
type CliFlags struct {
	Attributes bool
	// ResourceTimeout overrides how long each resource is given to run.
	// Zero means unset, and a negative duration means no timeout.
	ResourceTimeout time.Duration
	DryRun          bool
	DumpManifest    bool
	JSON            bool
	Quiet           bool
	Silent          bool
	Stdout          bool
}

// initCli loads command-line options
func initCli(c *CliFlags) {
	var (
		attributes      bool
		resourceTimeout time.Duration
		dryRun          bool
		dumpManifest    bool
		jsonOutput      bool
		quiet           bool
		silent          bool
		stdout          bool
	)

	flag.DurationVar(&resourceTimeout, "resource-timeout", envDuration("VIADUCT_RESOURCE_TIMEOUT"),
		"How long a single resource is given to run before the run gives up on it, overriding the manifest. A negative value means no timeout")
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
	c.ResourceTimeout = resourceTimeout
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

// envDuration reads a duration environment variable, returning zero when it is
// unset. A value that cannot be parsed is fatal rather than ignored, since
// silently keeping the old timeout is how you end up debugging the wrong thing.
func envDuration(name string) time.Duration {
	v, ok := os.LookupEnv(name)
	if !ok {
		return 0
	}

	d, err := time.ParseDuration(v)
	if err != nil {
		log.Fatalf("%s is not a duration: %s", name, v)
	}

	return d
}

// SetResourceTimeout overrides how long each resource is given to run.
func (c *CliFlags) SetResourceTimeout(d time.Duration) {
	c.ResourceTimeout = d
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
