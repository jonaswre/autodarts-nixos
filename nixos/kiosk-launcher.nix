{ pkgs }:
let
  controls = pkgs.runCommand "autodarts-kiosk-controls" { } ''
    install -Dm644 ${../kiosk/extension/manifest.json} $out/manifest.json
    install -Dm644 ${../kiosk/extension/controls.js} $out/controls.js
  '';
in pkgs.writeShellApplication {
  name = "autodarts-browser";
  runtimeInputs = [ pkgs.coreutils pkgs.gnugrep pkgs.wlr-randr pkgs.chromium ];
  text = ''
    drm_root="''${DRM_SYSFS_ROOT:-/sys/class/drm}"
    attempts="''${DISPLAY_WAIT_ATTEMPTS:-40}"
    browser="''${AUTODARTS_BROWSER:-chromium}"
    rotation="''${AUTODARTS_ROTATION:-normal}"

    for _ in $(seq 1 "$attempts"); do
      if grep -q '^connected$' "$drm_root"/card*-*/status 2>/dev/null; then
        if [[ "$rotation" != normal ]]; then
          for status_file in "$drm_root"/card*-*/status; do
            if [[ "$(cat "$status_file")" == connected ]]; then
              connector="$(basename "$(dirname "$status_file")")"
              connector="''${connector#card*-}"
              wlr-randr --output "$connector" --transform "$rotation"
              break
            fi
          done
        fi
        exec "$browser" \
          --ozone-platform=wayland \
          --enable-features=UseOzonePlatform \
          --kiosk \
          --no-first-run \
          --disable-session-crashed-bubble \
          --disable-component-update \
          --disable-background-networking \
          --password-store=basic \
          --disable-extensions-except=${controls} \
          --load-extension=${controls} \
          --app=https://play.autodarts.io/
      fi
      sleep 0.25
    done
    echo "No connected display; kiosk stays off" >&2
  '';
}
