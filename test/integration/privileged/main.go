// This configuration exercises resources that need block devices and mounts.
// privileged.sh runs it twice in a privileged container against loop
// devices, and the second run has to change nothing.
//
// It uses viaduct as a library, with its own flags, Apply and OnResult.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/surminus/viaduct"
	"github.com/surminus/viaduct/resources"
)

func main() {
	expectNoop := flag.Bool("expect-noop", false, "fail if anything changes")
	flag.Parse()

	fsDevice := os.Getenv("VIADUCT_FS_DEVICE")
	swapDevice := os.Getenv("VIADUCT_SWAP_DEVICE")
	usedDevice := os.Getenv("VIADUCT_USED_DEVICE")
	partitionedDevice := os.Getenv("VIADUCT_PARTITIONED_DEVICE")

	m := viaduct.New()

	fs := m.AddNamed("filesystem", resources.Mkfs(fsDevice, "ext4"))
	m.AddNamed("mount", &resources.Mount{
		Source:  fsDevice,
		Path:    "/mnt/viaduct data",
		FSType:  "ext4",
		Options: "defaults,nofail",
	}, fs)

	// A bind mount's mountinfo source is the device underneath, so a second
	// run must still see it as mounted.
	m.AddNamed("bind-mount", &resources.Mount{
		Source:  "/viaduct-bind-dir",
		Path:    "/mnt/viaduct-bind",
		FSType:  "none",
		Options: "bind",
	})

	m.AddNamed("swap", &resources.Swap{Device: swapDevice, Priority: 5})
	m.AddNamed("hostname", &resources.Hostname{Name: "viaduct-test", Transient: true})

	// A device with a different filesystem must be refused and left alone.
	// It is allowed to fail so the run carries on.
	used := m.AddNamed("refuse", resources.Mkfs(usedDevice, "ext4"))
	m.AllowFailure(used)

	// The same for a device with only a partition table, which blkid reports
	// with no filesystem type.
	partitioned := m.AddNamed("refuse-partitioned", resources.Mkfs(partitionedDevice, "ext4"))
	m.AllowFailure(partitioned)

	// A bind mounted file cannot be renamed over, so it is written in place.
	m.AddNamed("bind-mounted", resources.AppendLine("/etc/viaduct-bind", "written through a bind mount"))

	var changed []string

	report, err := m.Apply(context.Background(), viaduct.Options{
		Handler: viaduct.NewTextHandler(viaduct.TextOptions{}),
		OnResult: func(r viaduct.Result) {
			if r.Changed {
				changed = append(changed, string(r.ID))
			}
		},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	for _, r := range report.Final() {
		if r.ID != "refuse" && r.ID != "refuse-partitioned" {
			continue
		}

		if r.Status != viaduct.StatusFailed || !viaduct.IsPermanent(r.Err) || errors.Is(r.Err, os.ErrNotExist) {
			fmt.Fprintf(os.Stderr, "%s was not refused: %s %v\n", r.ID, r.Status, r.Err)
			os.Exit(1)
		}
	}

	if *expectNoop && len(changed) > 0 {
		fmt.Fprintf(os.Stderr, "expected no changes, but these changed: %v\n", changed)
		os.Exit(1)
	}
}
