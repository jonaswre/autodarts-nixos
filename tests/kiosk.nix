{ pkgs }:
let
  launcher = import ../nixos/kiosk-launcher.nix { inherit pkgs; };
in
pkgs.runCommand "autodarts-kiosk-behavior" { } ''
  mkdir -p "$out" connected/card0-HDMI-A-1 disconnected/card0-HDMI-A-1 bin
  echo connected > connected/card0-HDMI-A-1/status
  echo disconnected > disconnected/card0-HDMI-A-1/status

  cat > bin/browser <<'EOF'
  #!${pkgs.runtimeShell}
  printf '%s\n' "$@" > "$out/browser-arguments"
  EOF
  chmod +x bin/browser

  DRM_SYSFS_ROOT="$PWD/connected" \
    DISPLAY_WAIT_ATTEMPTS=1 \
    AUTODARTS_ROTATION=normal \
    AUTODARTS_BROWSER="$PWD/bin/browser" \
    ${launcher}/bin/autodarts-browser

  grep -Fx -- '--kiosk' "$out/browser-arguments"
  grep -Fx -- '--app=https://play.autodarts.io/' "$out/browser-arguments"
  grep -E -- '^--load-extension=/nix/store/' "$out/browser-arguments"

  DRM_SYSFS_ROOT="$PWD/disconnected" \
    DISPLAY_WAIT_ATTEMPTS=1 \
    AUTODARTS_ROTATION=normal \
    AUTODARTS_BROWSER="$PWD/bin/browser" \
    ${launcher}/bin/autodarts-browser

  touch "$out/headless-passed"
''
