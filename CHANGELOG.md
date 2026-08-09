# Changelog

All notable project changes are documented here.

## 0.1.0 - 2026-08-09

- Add a self-contained, guarded NixOS installer ISO for x86-64 systems.
- Support SATA (`/dev/sda`) and NVMe installation targets through detection.
- Run Autodarts Detection 1.0.7 as a sandboxed system service.
- Start a fast Wayland/Chromium kiosk only when a display is connected.
- Add configurable display rotation for portrait-mounted monitors.
- Add fullscreen switching between Play and the local Board Manager.
- Keep Google OAuth in a top-level browser context.
- Add persistent browser state, SSH-key provisioning, and console recovery.
