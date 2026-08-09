# Contributing

Changes should preserve the user journey: boot, detect a connected display,
open Play, switch to Board Manager, return to Play, and continue headlessly
when no display is attached.

Before submitting a change, run:

```console
nix flake check
nix build .#checks.x86_64-linux.kiosk
shellcheck install.sh scripts/*.sh
```

Changes to installation, storage selection, kiosk navigation, authentication,
or display handling require a behavioral regression check at the highest
practical level. Never weaken the exact disk-erasure confirmation.

Do not commit generated ISO files, Nix result links, credentials, browser
profiles, camera images, or Autodarts account data.
