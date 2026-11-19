package viaduct

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"

	flag "github.com/spf13/pflag"
)

// CliFlags configures the command-line toolset. Each Set method does the
// same as setting its field.
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

// Cli holds the command-line options used by Run. ParseFlags fills it from
// the command line and environment, keeping values already set on it, such
// as by SetDryRun, that the command line does not override.
var Cli CliFlags

var parsedFlags struct {
	once sync.Once
	err  error
}

// ParseFlags reads the command-line flags and their environment variables
// into Cli, and returns it. The command line is read once, on the first call.
// Run calls it; a program that uses Apply need not. An unknown flag is an
// error, as is --help, which prints the usage and returns pflag.ErrHelp.
func ParseFlags() (CliFlags, error) {
	parsedFlags.once.Do(func() {
		parsedFlags.err = parseFlags(&Cli, os.Args[1:])
	})

	return Cli, parsedFlags.err
}

// parseFlags applies the environment variables, and then the flags in args,
// to c. Anything neither sets keeps its value.
func parseFlags(c *CliFlags, args []string) error {
	if d, ok, err := envDuration("VIADUCT_RESOURCE_TIMEOUT"); err != nil {
		return err
	} else if ok {
		c.ResourceTimeout = d
	}

	if envBool("VIADUCT_STDOUT") {
		c.Stdout = true
	}

	if err := newFlagSet(c).Parse(args); err != nil {
		return err
	}

	if c.Silent && c.Quiet {
		return errors.New("cannot use --silent and --quiet together")
	}

	return nil
}

// newFlagSet returns the flags, each writing to its field in c and defaulting
// to the value already there.
func newFlagSet(c *CliFlags) *flag.FlagSet {
	fs := flag.NewFlagSet("viaduct", flag.ContinueOnError)

	fs.DurationVar(&c.ResourceTimeout, "resource-timeout", c.ResourceTimeout,
		"How long a single resource is given to run before the run gives up on it, overriding the manifest. A negative value means no timeout. Also set by VIADUCT_RESOURCE_TIMEOUT")
	fs.BoolVar(&c.DryRun, "dry-run", c.DryRun, "Report what would change without changing anything")
	fs.BoolVar(&c.Attributes, "attributes", c.Attributes, "Display known attributes")
	fs.BoolVar(&c.DumpManifest, "dump-manifest", c.DumpManifest, "Write every resource and its outcome to a file after the run")
	fs.BoolVar(&c.JSON, "json", c.JSON, "Output in JSON format")
	fs.BoolVar(&c.Quiet, "quiet", c.Quiet, "Quiet mode will only display errors during a run")
	fs.BoolVar(&c.Silent, "silent", c.Silent, "Silent mode will suppress all output except the final error")
	fs.BoolVar(&c.Stdout, "stdout", c.Stdout, "Log non-error output to STDOUT instead of STDERR (errors stay on STDERR). Also set by VIADUCT_STDOUT=true")

	return fs
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

// envDuration reads a duration environment variable, reporting whether it was
// set. A value that cannot be parsed is an error, since silently keeping the
// old timeout is how you end up debugging the wrong thing.
func envDuration(name string) (time.Duration, bool, error) {
	v, ok := os.LookupEnv(name)
	if !ok {
		return 0, false, nil
	}

	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, false, fmt.Errorf("%s is not a duration: %s", name, v)
	}

	return d, true, nil
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
