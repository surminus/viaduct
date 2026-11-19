package resources

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/surminus/viaduct"
)

const (
	// defaultTimeSyncWait is the default for MaxWait.
	defaultTimeSyncWait = 5 * time.Minute

	// defaultTimeSyncThreshold is the default for Threshold.
	defaultTimeSyncThreshold = 0.1

	// waitsyncInterval is the seconds between chronyc waitsync checks.
	waitsyncInterval = 10

	// stepThreshold is the offset, in seconds, above which the clock is
	// stepped rather than left to chrony to slew.
	stepThreshold = 1.0
)

// TimeSync synchronises the clock through chrony, which has to be running. A
// clock within Threshold is left alone. Otherwise TimeSync waits up to
// MaxWait for "chronyc waitsync" to report it synchronised, first stepping
// it with "chronyc makestep" when chrony is not synchronised or the clock is
// more than a second out, unless NoStep is set.
//
// The resource's timeout is MaxWait plus a minute, unless WithTimeout or
// Options.ResourceTimeout sets one.
type TimeSync struct {
	// MaxWait is how long to wait for the clock to synchronise. The default
	// is five minutes.
	MaxWait time.Duration

	// Threshold is the largest offset, in seconds, that counts as
	// synchronised. The default is 0.1.
	Threshold float64

	// NoStep never steps the clock, leaving chrony to slew it, so the time
	// never jumps. MaxWait has to cover the slew. The default is to step the
	// clock when chrony is not synchronised or it is more than a second out.
	NoStep bool
}

func (t *TimeSync) Description() string {
	return "chrony"
}

func (t *TimeSync) OperationName() string {
	return "Sync"
}

func (t *TimeSync) Params() *viaduct.ResourceParams {
	return &viaduct.ResourceParams{Timeout: t.wait() + time.Minute}
}

func (t *TimeSync) wait() time.Duration {
	if t.MaxWait <= 0 {
		return defaultTimeSyncWait
	}

	return t.MaxWait
}

func (t *TimeSync) PreflightChecks(ctx context.Context, log *viaduct.Logger) error {
	if t.MaxWait < 0 {
		return fmt.Errorf("cannot set MaxWait to a negative duration: %s", t.MaxWait)
	}

	if t.Threshold < 0 {
		return fmt.Errorf("cannot set Threshold to a negative number: %g", t.Threshold)
	}

	if t.MaxWait == 0 {
		t.MaxWait = defaultTimeSyncWait
	}

	if t.Threshold == 0 {
		t.Threshold = defaultTimeSyncThreshold
	}

	return requireRoot(ctx, "time sync")
}

func (t *TimeSync) Run(ctx context.Context, log *viaduct.Logger) error {
	out, err := viaduct.Command{Name: "chronyc", Args: []string{"-n", "tracking"}}.Output(ctx, log)
	if err != nil {
		return fmt.Errorf("chronyc tracking failed, is chronyd running? %w", err)
	}

	tracking := parseTracking(string(out))

	if tracking.synchronised && math.Abs(tracking.offset) <= t.Threshold {
		log.Noop("synchronised", "offset", tracking.offset)
		return nil
	}

	if !change(ctx, log, "synchronised", "offset", tracking.offset, "leap_status", tracking.leap) {
		return nil
	}

	// A small offset is slewed away by chrony, which a step would only jump
	// over. makestep fails while chrony has no source, which waitsync waits
	// for anyway.
	if !t.NoStep && (!tracking.synchronised || math.Abs(tracking.offset) > stepThreshold) {
		if err := runCommand(ctx, log, "chronyc", "makestep"); err != nil {
			log.Warn("makestep-failed", "error", err.Error())
		}
	}

	tries := int(math.Ceil(t.wait().Seconds() / waitsyncInterval))
	if tries < 1 {
		tries = 1
	}

	err = runCommand(ctx, log, "chronyc", "waitsync",
		strconv.Itoa(tries),
		strconv.FormatFloat(t.Threshold, 'f', -1, 64),
		"0",
		strconv.Itoa(waitsyncInterval),
	)
	if err != nil {
		return fmt.Errorf("the clock did not synchronise within %s: %w", t.wait(), err)
	}

	return nil
}

// tracking is the parsed output of "chronyc tracking".
type tracking struct {
	synchronised bool
	leap         string

	// offset is the clock's error in seconds, positive when fast.
	offset float64
}

// parseTracking parses "chronyc -n tracking". The clock counts as
// synchronised when the leap status is not "Not synchronised" and chrony
// has a reference.
func parseTracking(out string) tracking {
	var t tracking

	reference := ""

	for _, line := range strings.Split(out, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}

		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)

		switch key {
		case "Reference ID":
			reference, _, _ = strings.Cut(value, " ")
		case "Leap status":
			t.leap = value
		case "System time":
			// "0.000012345 seconds fast of NTP time"
			fields := strings.Fields(value)
			if len(fields) >= 3 {
				if n, err := strconv.ParseFloat(fields[0], 64); err == nil {
					t.offset = n
					if fields[2] == "slow" {
						t.offset = -n
					}
				}
			}
		}
	}

	t.synchronised = t.leap != "" && t.leap != "Not synchronised" && reference != "" && reference != "00000000"

	return t
}
