package resources

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/surminus/viaduct"
)

// Swap uses a device as swap space. It runs mkswap on an empty device,
// records the device in /etc/fstab by UUID so it is used at boot, and turns
// it on. The device must be a block device. One with anything else on it is
// an error and is never formatted.
type Swap struct {
	// Device is the block device, such as /dev/sdc.
	Device string

	// Priority is the swap priority, from 0 to 32767. Zero leaves it to the
	// kernel. A swap that is already on keeps the priority it has, and a
	// different one is logged as a warning.
	Priority int
}

func (s *Swap) Description() string {
	return s.Device
}

func (s *Swap) OperationName() string {
	return "Enable"
}

func (s *Swap) Params() *viaduct.ResourceParams {
	return viaduct.NewResourceParamsWithLockKey(viaduct.FstabLock)
}

func (s *Swap) PreflightChecks(ctx context.Context, log *viaduct.Logger) error {
	if s.Device == "" {
		return fmt.Errorf("required parameter: Device")
	}

	if !filepath.IsAbs(s.Device) {
		return fmt.Errorf("device must be an absolute path: %s", s.Device)
	}

	if s.Priority < 0 || s.Priority > 32767 {
		return fmt.Errorf("priority must be between 0 and 32767: %d", s.Priority)
	}

	return requireRoot(ctx, "swap")
}

func (s *Swap) Run(ctx context.Context, log *viaduct.Logger) error {
	env := viaduct.EnvFrom(ctx)

	active, priority, err := s.active(env)
	if err != nil {
		return err
	}

	p, err := probeDevice(ctx, log, s.Device)
	if err != nil {
		return err
	}

	switch {
	case p.Type == "swap" && p.PartitionTable == "":
		log.Noop("swap-exists", "device", s.Device)
	case p.Empty && !active:
		if !change(ctx, log, "mkswap", "device", s.Device) {
			// Without mkswap there is no UUID to record.
			log.Info("fstab-updated", "path", fstabPath, "device", s.Device)
			log.Info("swapon", "device", s.Device)

			return nil
		}

		if err := runCommand(ctx, log, "mkswap", s.Device); err != nil {
			return fmt.Errorf("mkswap failed on %s: %w", s.Device, err)
		}

		if p, err = probeDevice(ctx, log, s.Device); err != nil {
			return err
		}
	default:
		return viaduct.Permanent(fmt.Errorf("%s has %s and will not be used as swap: %s", s.Device, p.describe(), neverFormatted))
	}

	if p.UUID == "" {
		return fmt.Errorf("blkid found no UUID for the swap on %s", s.Device)
	}

	source := "UUID=" + p.UUID

	options := "sw"
	if s.Priority > 0 {
		options += ",pri=" + strconv.Itoa(s.Priority)
	}

	changed, err := ensureFstabEntry(ctx, log, fstabEntry{
		Source:  source,
		Path:    "none",
		FSType:  "swap",
		Options: options,
	}, func(fields []string) bool {
		return len(fields) > 2 && fields[2] == "swap" &&
			(fields[0] == source || sameDevice(env, fields[0], s.Device))
	})
	if err != nil {
		return err
	}

	// systemd swap units only match fstab after a reload.
	if changed {
		if err := reloadSystemd(ctx, log); err != nil {
			return err
		}
	}

	if active {
		log.Noop("swapon", "device", s.Device)

		// Changing the priority of an active swap means turning it off, which
		// is not safe to do on a running host
		if s.Priority > 0 && priority != s.Priority {
			log.Warn("priority-differs", "device", s.Device, "priority", priority, "want", s.Priority)
		}

		return nil
	}

	if !change(ctx, log, "swapon", "device", s.Device) {
		return nil
	}

	args := []string{"swapon"}
	if s.Priority > 0 {
		args = append(args, "-p", strconv.Itoa(s.Priority))
	}

	if err := runCommand(ctx, log, append(args, s.Device)...); err != nil {
		return fmt.Errorf("swapon failed on %s: %w", s.Device, err)
	}

	return nil
}

// active reports whether the device is in use as swap, and its priority,
// comparing resolved paths as /proc/swaps may list another name.
func (s *Swap) active(env viaduct.Env) (bool, int, error) {
	content, err := os.ReadFile(env.Path("/proc/swaps"))
	if err != nil {
		return false, 0, err
	}

	for i, line := range strings.Split(string(content), "\n") {
		fields := strings.Fields(line)
		if i == 0 || len(fields) == 0 {
			continue
		}

		if sameDevice(env, fstabUnescape(fields[0]), s.Device) {
			var priority int
			if len(fields) > 4 {
				priority, _ = strconv.Atoi(fields[4])
			}

			return true, priority, nil
		}
	}

	return false, 0, nil
}
