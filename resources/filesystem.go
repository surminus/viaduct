package resources

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"

	"github.com/surminus/viaduct"
)

// Filesystem creates a filesystem on an empty block device. A device that
// already has the same type of filesystem is left alone, with msdos, fat and
// vfat counting as the same. One with anything else on
// it, such as another filesystem or a partition table, is an error and is
// never formatted.
type Filesystem struct {
	// Device is the block device, such as /dev/sdb.
	Device string

	// Type is the filesystem type, such as ext4 or xfs, passed to "mkfs -t".
	Type string

	// Options are extra arguments for mkfs, such as "-L", "data". Optional.
	Options []string

	// uuid is the filesystem's UUID, once Run has found or created it.
	uuid string
}

// UUID returns the UUID of the filesystem on the device, once Run has
// succeeded, for mounting it by UUID. It is empty before then, and after a
// dry run that would have formatted the device.
func (f *Filesystem) UUID() string {
	return f.uuid
}

// Mkfs creates a Filesystem of the given type on a device.
func Mkfs(device, fstype string) *Filesystem {
	return &Filesystem{Device: device, Type: fstype}
}

// fsTypePattern restricts a filesystem type to what is safe in a program
// name.
var fsTypePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

func (f *Filesystem) Description() string {
	return f.Device
}

func (f *Filesystem) OperationName() string {
	return "Create"
}

// Params takes FstabLock.
func (f *Filesystem) Params() *viaduct.ResourceParams {
	return viaduct.NewResourceParamsWithLockKey(viaduct.FstabLock)
}

func (f *Filesystem) PreflightChecks(ctx context.Context, log *viaduct.Logger) error {
	if f.Device == "" {
		return fmt.Errorf("required parameter: Device")
	}

	if !filepath.IsAbs(f.Device) {
		return fmt.Errorf("device must be an absolute path: %s", f.Device)
	}

	if f.Type == "" {
		return fmt.Errorf("required parameter: Type")
	}

	if !fsTypePattern.MatchString(f.Type) {
		return fmt.Errorf("invalid filesystem type: %s", f.Type)
	}

	return requireRoot(ctx, "filesystem")
}

func (f *Filesystem) Run(ctx context.Context, log *viaduct.Logger) error {
	p, err := probeDevice(ctx, log, f.Device)
	if err != nil {
		return err
	}

	if !p.Empty {
		if blkidType(p.Type) == blkidType(f.Type) && p.PartitionTable == "" {
			log.Noop("up-to-date", "device", f.Device, "type", f.Type)
			f.uuid = p.UUID

			return nil
		}

		return viaduct.Permanent(fmt.Errorf("%s has %s and will not be formatted as %s: %s", f.Device, p.describe(), f.Type, neverFormatted))
	}

	if !change(ctx, log, "formatted", "device", f.Device, "type", f.Type) {
		return nil
	}

	// The probe is the only guard, as mkfs.ext4 overwrites without a
	// terminal. No force flag is passed, for tools that check, such as
	// mkfs.xfs.
	args := append([]string{"-t", f.Type}, f.Options...)
	args = append(args, f.Device)

	if err := runCommand(ctx, log, append([]string{"mkfs"}, args...)...); err != nil {
		return fmt.Errorf("mkfs failed on %s: %w", f.Device, err)
	}

	if p, err = probeDevice(ctx, log, f.Device); err != nil {
		return fmt.Errorf("reading the filesystem mkfs created: %w", err)
	}

	f.uuid = p.UUID

	return nil
}

// neverFormatted ends the error for a device that has something on it.
const neverFormatted = "it is never formatted automatically; wipe it (for example with wipefs -a) if its contents are disposable"

// blkidType returns the name blkid reports for a filesystem type, so a type
// given to mkfs under another name still matches.
func blkidType(fstype string) string {
	switch fstype {
	case "msdos", "fat", "vfat":
		return "vfat"
	default:
		return fstype
	}
}
