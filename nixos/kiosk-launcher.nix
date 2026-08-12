{ pkgs }:
let
  displayScale = import ./display-scale.nix { inherit pkgs; };
  controls = pkgs.runCommand "autodarts-kiosk-controls" { } ''
    install -Dm644 ${../kiosk/extension/manifest.json} $out/manifest.json
    install -Dm644 ${../kiosk/extension/controls.js} $out/controls.js
    install -Dm644 ${../kiosk/extension/background.js} $out/background.js
    install -Dm644 ${../kiosk/extension/websocket-capture.js} $out/websocket-capture.js
    install -Dm644 ${../kiosk/extension/capture-bridge.js} $out/capture-bridge.js
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
    randr="''${AUTODARTS_WLR_RANDR:-wlr-randr}"
    rotation="''${AUTODARTS_ROTATION:-normal}"
    play_url="''${AUTODARTS_PLAY_URL:-https://play.autodarts.io/}"
    probe_url="''${AUTODARTS_PROBE_URL:-$play_url}"
    onboarding_url="''${AUTODARTS_ONBOARDING_URL:-http://127.0.0.1:3182}"
    onboarding_enabled="''${AUTODARTS_ONBOARDING_ENABLED:-true}"

    for _ in $(seq 1 "$attempts"); do
      if grep -q '^connected$' "$drm_root"/card*-*/status 2>/dev/null; then
        for status_file in "$drm_root"/card*-*/status; do
          if [[ "$(cat "$status_file")" == connected ]]; then
            connector="$(basename "$(dirname "$status_file")")"
            connector="''${connector#card*-}"
            mode="$(head -n 1 "$(dirname "$status_file")/modes" 2>/dev/null || true)"
            scale="$(${displayScale}/bin/autodarts-display-scale "$mode")"
            "$randr" --output "$connector" --scale "$scale"
            if [[ "$rotation" != normal ]]; then
              "$randr" --output "$connector" --transform "$rotation"
            fi
            break
          fi
        done

        if [[ "$onboarding_enabled" == true ]]; then
          until curl --silent --fail --output /dev/null --max-time 1 \
            "$onboarding_url/api/configured"; do
            if curl --silent --fail --output /dev/null --max-time 1 \
              "$onboarding_url/api/setup-required"; then
              exec "$browser" \
                --ozone-platform=wayland \
                --enable-features=UseOzonePlatform \
                --kiosk \
                --no-first-run \
                --disable-session-crashed-bubble \
                --disable-component-update \
                --disable-background-networking \
                --password-store=basic \
                --app="$onboarding_url/device"
            fi
            sleep 0.25
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
