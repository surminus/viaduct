package resources

import (
	"context"
	"fmt"
	"strings"

	"github.com/surminus/viaduct"
)

// probe is what blkid finds on a device
type probe struct {
	// Empty is true when blkid found nothing at all on the device
	Empty bool

	// Type is the filesystem type, such as "ext4" or "swap"
	Type string

	// PartitionTable is the partition table type, such as "gpt"
	PartitionTable string

	UUID string
}

// probeDevice reads a device with "blkid -p", which bypasses the cache. The
// device must exist, as blkid reports a missing device like an empty one.
// Any other error is permanent, so an unreadable device is never formatted.
func probeDevice(ctx context.Context, log *viaduct.Logger, device string) (probe, error) {
	env := viaduct.EnvFrom(ctx)

	block, err := env.IsBlockDevice(device)
	if err != nil {
		return probe{}, fmt.Errorf("device %s: %w", device, err)
	}

	if !block {
		return probe{}, viaduct.Permanent(fmt.Errorf("%s is not a block device", device))
	}

	out, err := viaduct.Command{Name: "blkid", Args: []string{"-p", "-o", "export", device}}.Output(ctx, log)

	if code, ok := viaduct.ExitCode(err); ok && code == 2 {
		return probe{Empty: true}, nil
	}

	if err != nil {
		return probe{}, viaduct.Permanent(fmt.Errorf("blkid could not read %s: %w", device, err))
	}

	var p probe

	for _, line := range strings.Split(string(out), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}

		switch key {
		case "TYPE":
			p.Type = value
		case "PTTYPE":
			p.PartitionTable = value
		case "UUID":
			p.UUID = value
		}
	}

	return p, nil
}

// describe says what a probe found, for errors.
func (p probe) describe() string {
	switch {
	case p.Type != "":
		return "a filesystem of type " + p.Type
	case p.PartitionTable != "":
		return "a partition table of type " + p.PartitionTable
	case p.Empty:
		return "nothing blkid recognises"
	default:
		return "an unrecognised signature"
	}
}
