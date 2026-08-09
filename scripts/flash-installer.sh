#!/usr/bin/env bash
set -euo pipefail

if [[ $EUID -ne 0 || $# -ne 2 ]]; then
  echo "Usage: sudo $0 IMAGE.iso /dev/sdX" >&2
  exit 2
fi

image=$(readlink -f "$1")
device=$(readlink -f "$2")

[[ -f "$image" ]] || { echo "Image not found: $image" >&2; exit 2; }
[[ -b "$device" ]] || { echo "Not a block device: $device" >&2; exit 2; }

device_name=$(basename "$device")
removable_file="/sys/class/block/$device_name/removable"
if [[ ! -r "$removable_file" || "$(<"$removable_file")" != 1 ]]; then
  echo "Refusing to flash a device not marked removable: $device" >&2
  exit 1
fi

root_source=$(findmnt -nro SOURCE /)
root_parent=$(lsblk -nro PKNAME "$root_source" | tail -n1)
if [[ -n "$root_parent" && "$device" == "/dev/$root_parent" ]]; then
  echo "Refusing to overwrite the disk backing the root filesystem." >&2
  exit 1
fi

echo "Image:  $image"
lsblk -dno NAME,SIZE,MODEL,TRAN,RM "$device"
echo "This permanently erases $device. Type exactly: FLASH $device"
read -r confirmation
[[ "$confirmation" == "FLASH $device" ]] || { echo "Cancelled"; exit 1; }

while read -r mountpoint; do
  [[ -n "$mountpoint" ]] && umount "$mountpoint"
done < <(lsblk -nrpo MOUNTPOINTS "$device" | awk 'NF')

dd if="$image" of="$device" bs=4M conv=fsync status=progress
sync

expected=$(sha256sum "$image" | awk '{print $1}')
image_size=$(stat -c %s "$image")
actual=$(head -c "$image_size" "$device" | sha256sum | awk '{print $1}')
if [[ "$actual" != "$expected" ]]; then
  echo "Readback verification failed: expected $expected, got $actual" >&2
  exit 1
fi

echo "Flash and readback verification succeeded: $actual"
