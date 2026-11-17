#!/usr/bin/env bash
# Runs the resources that need block devices and mounts in a privileged
# container, against loop devices. The swap device is turned on in the host
# kernel while the test runs, and turned off again afterwards.
set -euo pipefail

cd "$(dirname "$0")"

IMAGE=${IMAGE:-ubuntu:24.04}

echo "Building privileged integration binary"
CGO_ENABLED=0 GOOS=linux go build -o viaduct-privileged ./privileged

docker run --rm --privileged -v "$PWD:/test:ro" "$IMAGE" bash -euo pipefail -c '
command -v sfdisk >/dev/null || { apt-get update -qq && apt-get install -y -qq fdisk; } >/dev/null

truncate -s 64M /fs.img /swap.img /used.img /partitioned.img

FS= SWAP= USED= PARTITIONED=

# Set before the first loop device, so one failing to attach does not leave
# the others attached in the host kernel
cleanup() {
	if [ -n "$SWAP" ]; then swapoff "$SWAP" 2>/dev/null || true; fi
	umount "/mnt/viaduct data" 2>/dev/null || true
	umount /etc/viaduct-bind 2>/dev/null || true
	umount /mnt/viaduct-bind 2>/dev/null || true
	for dev in "$FS" "$SWAP" "$USED" "$PARTITIONED"; do
		if [ -n "$dev" ]; then losetup -d "$dev" || true; fi
	done
}
trap cleanup EXIT

FS=$(losetup -f --show /fs.img)
SWAP=$(losetup -f --show /swap.img)
USED=$(losetup -f --show /used.img)
PARTITIONED=$(losetup -f --show /partitioned.img)

mkfs.ext2 -q "$USED"
echo "label: gpt" | sfdisk -q "$PARTITIONED" 2>/dev/null || true
test "$(blkid -p -o value -s PTTYPE "$PARTITIONED")" = gpt

echo "before" > /viaduct-bind-source
touch /etc/viaduct-bind
mount --bind /viaduct-bind-source /etc/viaduct-bind
mkdir /viaduct-bind-dir
touch /viaduct-bind-dir/marker

export VIADUCT_FS_DEVICE=$FS VIADUCT_SWAP_DEVICE=$SWAP VIADUCT_USED_DEVICE=$USED VIADUCT_PARTITIONED_DEVICE=$PARTITIONED

echo "=== First run"
/test/viaduct-privileged

echo "=== Second run, which must change nothing"
/test/viaduct-privileged --expect-noop

echo "=== Verifying"
test "$(blkid -p -o value -s TYPE "$FS")" = ext4
test "$(blkid -p -o value -s TYPE "$USED")" = ext2
test "$(blkid -p -o value -s PTTYPE "$PARTITIONED")" = gpt
test -z "$(blkid -p -o value -s TYPE "$PARTITIONED")"
test "$(cat /viaduct-bind-source)" = "$(printf "before\nwritten through a bind mount")"
findmnt --source "$FS" --target "/mnt/viaduct data" >/dev/null
test -f /mnt/viaduct-bind/marker
grep -q "^/viaduct-bind-dir /mnt/viaduct-bind none bind 0 0$" /etc/fstab
grep -q "^$SWAP " /proc/swaps
grep -q "^$FS /mnt/viaduct\\\\040data ext4 defaults,nofail 0 0$" /etc/fstab
grep -q "^UUID=$(blkid -p -o value -s UUID "$SWAP") none swap sw,pri=5 0 0$" /etc/fstab
test "$(hostname)" = viaduct-test
echo "All privileged integration tests passed"
'
