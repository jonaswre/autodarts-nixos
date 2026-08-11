{ pkgs }:
let
  controls = pkgs.runCommand "autodarts-kiosk-controls" { } ''
    install -Dm644 ${../kiosk/extension/manifest.json} $out/manifest.json
    install -Dm644 ${../kiosk/extension/controls.js} $out/controls.js
  '';
  loadingPage = pkgs.runCommand "autodarts-kiosk-loading" { } ''
    install -Dm644 ${../kiosk/loading.html} $out/loading.html
  '';
in
pkgs.writeShellApplication {
  name = "autodarts-browser";
  runtimeInputs = [
    pkgs.coreutils
    pkgs.curl
    pkgs.gnugrep
    pkgs.wlr-randr
    pkgs.chromium
  ];
  text = ''
    drm_root="''${DRM_SYSFS_ROOT:-/sys/class/drm}"
    attempts="''${DISPLAY_WAIT_ATTEMPTS:-40}"
    browser="''${AUTODARTS_BROWSER:-chromium}"
    rotation="''${AUTODARTS_ROTATION:-normal}"
    play_url="''${AUTODARTS_PLAY_URL:-https://play.autodarts.io/}"
    probe_url="''${AUTODARTS_PROBE_URL:-$play_url}"

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
        splash_profile="$XDG_RUNTIME_DIR/splash-profile"
        rm -rf "$splash_profile"
        "$browser" \
          --ozone-platform=wayland \
          --enable-features=UseOzonePlatform \
          --kiosk \
          --no-first-run \
          --disable-session-crashed-bubble \
          --disable-component-update \
          --disable-background-networking \
          --password-store=basic \
          --user-data-dir="$splash_profile" \
          --app=file://${loadingPage}/loading.html &
        splash_pid=$!
        trap 'kill "$splash_pid" 2>/dev/null || true' EXIT INT TERM

        until curl --silent --show-error --fail \
          --output /dev/null --max-time 2 "$probe_url"; do
          sleep 0.75
        done

        kill "$splash_pid" 2>/dev/null || true
        wait "$splash_pid" 2>/dev/null || true
        trap - EXIT INT TERM
        rm -rf "$splash_profile"

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
          --app="$play_url"
      fi
      sleep 0.25
    done
    echo "No connected display; kiosk stays off" >&2
  '';
}
