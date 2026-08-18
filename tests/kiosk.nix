{ pkgs }:
let
  launcher = import ../nixos/kiosk-launcher.nix { inherit pkgs; };
in
pkgs.runCommand "autodarts-kiosk-behavior"
  {
    nativeBuildInputs = [
      pkgs.netcat-openbsd
      pkgs.nodejs
    ];
  }
  ''
    mkdir -p "$out" connected/card0-HDMI-A-1 disconnected/card0-HDMI-A-1 bin runtime
    echo connected > connected/card0-HDMI-A-1/status
    echo 3840x2160 > connected/card0-HDMI-A-1/modes
    echo disconnected > disconnected/card0-HDMI-A-1/status

    cat > bin/browser <<'EOF'
    #!${pkgs.runtimeShell}
    if printf '%s\n' "$@" | grep -q '^--app=file://'; then
      printf '%s\n' "$@" > "$out/splash-arguments"
      trap 'exit 0' TERM INT
      while true; do sleep 1; done
    fi
    printf '%s\n' "$@" > "$out/browser-arguments"
    EOF
    chmod +x bin/browser

    cat > bin/wlr-randr <<'EOF'
    #!${pkgs.runtimeShell}
    printf '%s\n' "$@" > "$out/randr-arguments"
    EOF
    chmod +x bin/wlr-randr

    (
      sleep 1
      printf 'HTTP/1.1 204 No Content\r\nConnection: close\r\n\r\n' | nc -l 127.0.0.1 18080
    ) &

    DRM_SYSFS_ROOT="$PWD/connected" \
      DISPLAY_WAIT_ATTEMPTS=1 \
      AUTODARTS_ROTATION=normal \
      AUTODARTS_ONBOARDING_ENABLED=false \
      AUTODARTS_PROBE_URL=http://127.0.0.1:18080/ \
      AUTODARTS_BROWSER="$PWD/bin/browser" \
      AUTODARTS_WLR_RANDR="$PWD/bin/wlr-randr" \
      XDG_RUNTIME_DIR="$PWD/runtime" \
      ${launcher}/bin/autodarts-browser

    grep -E -- '^--app=file:///nix/store/.*/loading.html$' "$out/splash-arguments"
    grep -Fx -- '--kiosk' "$out/browser-arguments"
    grep -Fx -- '--restore-last-session' "$out/browser-arguments"
    grep -Fx -- '--app=https://play.autodarts.io/' "$out/browser-arguments"
    grep -E -- '^--load-extension=/nix/store/' "$out/browser-arguments"
    grep -Fx -- '--scale' "$out/randr-arguments"
    grep -Fx -- '2' "$out/randr-arguments"

    node ${./kiosk-controls.js} ${../kiosk/extension/controls.js}

    DRM_SYSFS_ROOT="$PWD/disconnected" \
      DISPLAY_WAIT_ATTEMPTS=1 \
      AUTODARTS_ROTATION=normal \
      AUTODARTS_BROWSER="$PWD/bin/browser" \
      ${launcher}/bin/autodarts-browser

    touch "$out/headless-passed"
  ''
