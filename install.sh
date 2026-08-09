#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 || "$1" != /dev/* ]]; then
  echo "Usage: sudo ./install.sh /dev/nvme0n1" >&2
  exit 2
fi

disk="$1"
if [[ ! -b "$disk" ]]; then
  echo "$disk is not a block device" >&2
  exit 2
fi

echo "This permanently erases $disk. Type its full path to continue:"
read -r confirmation
[[ "$confirmation" == "$disk" ]] || { echo "Cancelled"; exit 1; }

nix --extra-experimental-features 'nix-command flakes' run \
  github:nix-community/disko -- \
  --mode disko ./nixos/disk.nix --argstr disk "$disk"

install -d /mnt/etc/nixos
cp -a flake.nix flake.lock nixos tests scripts README.md install.sh /mnt/etc/nixos/

nixos-install --no-root-password --flake /mnt/etc/nixos#beelink \
  --option extra-experimental-features 'nix-command flakes'

echo "Installed. Reboot after removing the installer USB."
