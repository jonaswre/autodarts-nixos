# Autodarts NixOS Appliance

[![NixOS](https://img.shields.io/badge/NixOS-26.05-5277C3?logo=nixos&logoColor=white)](https://nixos.org/)
[![CI](https://github.com/jonaswre/autodarts-nixos/actions/workflows/check.yml/badge.svg)](https://github.com/jonaswre/autodarts-nixos/actions/workflows/check.yml)
[![Release](https://img.shields.io/github/v/release/jonaswre/autodarts-nixos)](https://github.com/jonaswre/autodarts-nixos/releases/latest)
[![License](https://img.shields.io/github/license/jonaswre/autodarts-nixos)](LICENSE)

A reproducible, fast-booting Linux appliance for [Autodarts](https://autodarts.io/),
designed for the Beelink Mini S12 and similar x86-64 Intel mini PCs.

Connect the cameras and a display, switch on the computer, and the appliance
boots directly into Autodarts Play. There is no desktop, browser chrome, or
Windows installation to maintain. Autodarts Detection also runs when no display
is connected.

> [!WARNING]
> Installation erases the selected internal disk. Read the target device shown
> by the installer before entering the confirmation phrase.

This is an independent community project. It is not affiliated with or endorsed
by Autodarts.

## What you get

- Full-screen Chromium kiosk without tabs or an address bar
- Autodarts Play and the local Board Manager on the same display
- Top-level Google sign-in instead of an embedded login flow
- Persistent browser profile and login between reboots
- Headless Autodarts Detection and remote Board Manager access
- Display rotation for landscape and portrait installations
- Fast boot using systemd-boot, Cage, Wayland, and Intel fastboot
- Reproducible system and installer images built with Nix
- Guarded disk installation and USB flashing workflows

```text
USB cameras ──> Autodarts Detection ──> Autodarts services
                         │
                         └──> Board Manager :3180
                                      │
Display ──> Cage + Chromium ──> Play / Board Manager fullscreen
```

## Supported hardware

The supplied configuration targets:

- Beelink Mini S12 with Intel N95
- Beelink Mini S12 Pro with Intel N100
- UEFI firmware
- SATA or NVMe internal storage
- Ethernet or Linux-supported Wi-Fi
- UVC-compatible USB cameras
- HDMI display, optionally mounted in portrait orientation

Other x86-64 Intel systems may work, but have not received the same hardware
testing.

## Quick start

You need a Linux computer, a USB drive of at least 4 GB, and the target mini PC.

1. Download the ISO and `SHA256SUMS` from the
   [latest release](https://github.com/jonaswre/autodarts-nixos/releases/latest).
2. Verify the download:

   ```console
   sha256sum --check SHA256SUMS
   ```

3. Identify the USB device carefully:

   ```console
   lsblk -o NAME,SIZE,MODEL,TRAN,RM,MOUNTPOINTS
   ```

4. Clone this repository and use the guarded flash helper:

   ```console
   git clone https://github.com/jonaswre/autodarts-nixos.git
   cd autodarts-nixos
   sudo ./scripts/flash-installer.sh \
     ~/Downloads/autodarts-beelink-appliance-0.1.0.iso /dev/sdX
   ```

5. Boot the Beelink from the USB drive and run:

   ```console
   autodarts-install
   ```

6. Confirm the internal target disk, optionally paste an SSH public key, and
   wait for installation to complete.
7. Shut down, remove the USB drive, and boot from the internal disk.

The installer automatically selects the disk only when exactly one
non-removable target exists. With multiple internal disks, pass the target
explicitly, for example `autodarts-install /dev/sda`.

The first Autodarts sign-in may require a keyboard and mouse. After setup, the
kiosk is intended for normal keyboard-free operation.

## Display rotation

Landscape is the safe default. For a monitor physically rotated clockwise, set
the rotation to `90` in `nixos/configuration.nix` before building:

```nix
services.autodarts-kiosk.rotation = "90";
```

Supported values are `normal`, `90`, `180`, and `270`.

## Build the installer

On NixOS or Linux with [Nix](https://nixos.org/download/) and flakes enabled:

```console
git clone https://github.com/jonaswre/autodarts-nixos.git
cd autodarts-nixos
nix build .#installer-iso
```

The ISO appears under `result/iso/`. Flash it with:

```console
sudo ./scripts/flash-installer.sh result/iso/*.iso /dev/sdX
```

The helper refuses disks not marked removable and the disk backing `/`, asks
for the exact phrase `FLASH /dev/sdX`, writes the image, and verifies every
written byte with SHA-256.

## Continuous delivery

Every push to `main` builds the complete installer ISO in GitHub Actions. The
ISO and `SHA256SUMS` are available as a workflow artifact for seven days.

Pushing a tag matching `v*` builds the same pinned source and publishes the ISO
and checksum on the corresponding GitHub release. The build uses one Nix job
and two compiler cores so it remains within the memory available on hosted
runners. The workflow can also be started manually from the Actions page.

## Install from an existing NixOS environment

The generic installer is useful when the machine is already in a NixOS live
environment with network access:

```console
git clone https://github.com/jonaswre/autodarts-nixos.git
cd autodarts-nixos
sudo ./install.sh /dev/nvme0n1
```

Replace the example device with the internal disk reported by `lsblk`.

## Administration

The physical console automatically logs in as `admin` for local recovery.
Remote access is key-only and is enabled when a public key is supplied during
installation; root SSH login and password SSH login are disabled.

```console
ssh admin@autodarts
journalctl -u autodarts -f
journalctl -u autodarts-kiosk -f
sudo systemctl restart autodarts-kiosk
```

Board Manager is available at `http://autodarts:3180` or at the appliance IP.
To find the current IP locally:

```console
hostname -I
```

Persistent data is stored in:

- `/var/lib/autodarts`
- `/var/lib/autodarts-kiosk`

Back up both directories before reinstalling.

## Updating

The NixOS input and Autodarts Detection release are pinned for reproducibility.
Review changes before rebuilding:

```console
nix flake update
nix flake check
nix build .#installer-iso
```

Autodarts Detection's version, download URL, and hash are kept together in
`nixos/autodarts.nix`. The appliance intentionally has no mutable background
updater.

## Testing

```console
nix flake check
nix build .#checks.x86_64-linux.kiosk
nix build .#checks.x86_64-linux.source
nix build .#installer-iso
```

The test suite evaluates the complete NixOS configuration and exercises the
user-visible connected-display and headless boot paths. It also validates kiosk
navigation, extension origins, installer safeguards, JSON, and shell scripts.

## Project layout

| Path | Purpose |
| --- | --- |
| `nixos/` | Appliance, disk, kiosk, and installer modules |
| `kiosk/extension/` | Full-screen Play and Board Manager controls |
| `scripts/flash-installer.sh` | Guarded Linux USB writer and verifier |
| `tests/` | NixOS user-journey and source-policy checks |
| `.github/workflows/` | Checks, installer builds, artifacts, and releases |
| `flake.nix` | Public systems, packages, checks, and module outputs |

## Contributing and security

Contributions are welcome; see [CONTRIBUTING.md](CONTRIBUTING.md). Please report
security issues using the process in [SECURITY.md](SECURITY.md), not through a
public issue.

## License

The NixOS configuration, installer, and kiosk integration are available under
the [MIT License](LICENSE). The downloaded Autodarts Detection binary remains
subject to its publisher's terms.
