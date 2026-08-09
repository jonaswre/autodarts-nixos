# Autodarts NixOS appliance

A reproducible, fast-booting Autodarts appliance for the Beelink Mini S12 and
similar x86-64 Intel mini PCs. It runs Autodarts Detection headlessly and opens
[Autodarts Play](https://play.autodarts.io/) as a keyboard-free Wayland kiosk
whenever a display is connected.

This community project is not affiliated with or endorsed by Autodarts. The
NixOS configuration and kiosk controls are MIT licensed. The downloaded
Autodarts Detection binary remains subject to its publisher's terms.

## User experience

- Boots directly into a fullscreen Chromium app without tabs or an address bar.
- Keeps Google sign-in in the top-level page so OAuth works normally.
- Adds a **Board** control that opens the local Board Manager fullscreen; **×**
  returns to Play.
- Persists the Chromium profile and login across reboots and system updates.
- Supports `normal`, `90`, `180`, and `270` display transforms.
- Runs detection without a monitor and exposes Board Manager on TCP port 3180.

## Hardware target

The supplied system targets Beelink Mini S12 N95 and Mini S12 Pro N100 models:

- UEFI firmware
- Intel Alder Lake-N graphics
- SATA or NVMe internal storage
- Ethernet or supported Wi-Fi
- UVC USB cameras
- One connected HDMI display for kiosk mode

Other x86-64 Intel systems may work but have not received the same runtime
verification.

## Build the installer

On NixOS or another Linux machine with Nix and flakes enabled:

```console
nix build .#installer-iso
```

The image is produced under `result/iso/`. Flash and read it back using the
guarded Linux helper (replace `/dev/sdX` with the removable USB disk):

```console
sudo ./scripts/flash-installer.sh \
  result/iso/autodarts-beelink-installer.iso /dev/sdX
```

The helper refuses non-removable devices and the root disk, displays the target,
requires `FLASH /dev/sdX`, and verifies the written bytes by SHA-256.

The installer:

1. Lists every detected disk.
2. Automatically selects a target only when exactly one non-removable disk
   exists.
3. Requires the exact phrase `ERASE /dev/DEVICE` before partitioning.
4. Optionally accepts an SSH public key for `admin`.
5. Installs the complete system closure without downloading packages.

For machines with multiple internal disks, run `autodarts-install /dev/DEVICE`
from the installer shell.

## Configuration

The release defaults to landscape orientation. For a monitor physically mounted
clockwise, edit `nixos/configuration.nix` before building:

```nix
services.autodarts-kiosk.rotation = "90";
```

Valid values are `normal`, `90`, `180`, and `270`.

The public configuration contains no password or personal SSH key. The physical
console logs into `admin` automatically for recovery; password-based SSH and root
SSH login are disabled. Paste a public key when the installer asks to enable
remote administration.

## Generic NixOS installation

From a current NixOS installer environment:

```console
chmod +x install.sh
sudo ./install.sh /dev/sda
```

Replace `/dev/sda` with the internal target reported by `lsblk`. This path
requires network access because it evaluates the flake during installation.

## Administration

```console
ssh admin@autodarts
journalctl -u autodarts -f
journalctl -u autodarts-kiosk -f
sudo nixos-rebuild switch --flake /etc/nixos#beelink
```

Board Manager is available at `http://autodarts:3180` or the appliance's IP.
Persistent state lives in `/var/lib/autodarts` and
`/var/lib/autodarts-kiosk`. Back up both directories before reinstalling.

## Fast boot

The appliance uses systemd-boot with a zero-second menu, a systemd initrd,
`i915.fastboot=1`, Cage instead of a desktop environment, and no global
network-online wait. Detection and SSH start independently of the display.

For best results, enable UEFI Fast Boot and automatic power-on in firmware,
place the internal disk first in the boot order, and disable unused PXE boot.

## Updating

Inputs and Autodarts Detection are deliberately pinned:

```console
nix flake update
nix flake check
nix build .#installer-iso
```

Autodarts Detection 1.0.7 and its published SHA-256 are recorded in
`nixos/autodarts.nix`. Update the version, URL, and hash together. The appliance
does not run a mutable root updater.

## Validation

```console
nix flake check
nix build .#checks.x86_64-linux.kiosk
nix build .#installer-iso
```

The checks evaluate the complete NixOS system, exercise connected-display and
headless kiosk branches, validate the extension origins and navigation, lint the
generic installer, and build the bootable ISO.
