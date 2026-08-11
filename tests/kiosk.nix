{ pkgs }:
let
  launcher = import ../nixos/kiosk-launcher.nix { inherit pkgs; };
in
pkgs.runCommand "autodarts-kiosk-behavior" { nativeBuildInputs = [ pkgs.python3 ]; } ''
  mkdir -p "$out" connected/card0-HDMI-A-1 disconnected/card0-HDMI-A-1 bin runtime
  echo connected > connected/card0-HDMI-A-1/status
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

  (
    sleep 1
    ${pkgs.python3}/bin/python - <<'PY'
  from http.server import BaseHTTPRequestHandler, HTTPServer

  class Ready(BaseHTTPRequestHandler):
      def do_GET(self):
          self.send_response(204)
          self.end_headers()

      def log_message(self, *_args):
          pass

  server = HTTPServer(("127.0.0.1", 18080), Ready)
  server.timeout = 10
  server.handle_request()
  PY
  ) &

  DRM_SYSFS_ROOT="$PWD/connected" \
    DISPLAY_WAIT_ATTEMPTS=1 \
    AUTODARTS_ROTATION=normal \
    AUTODARTS_ONBOARDING_ENABLED=false \
    AUTODARTS_PROBE_URL=http://127.0.0.1:18080/ \
    AUTODARTS_BROWSER="$PWD/bin/browser" \
    XDG_RUNTIME_DIR="$PWD/runtime" \
    ${launcher}/bin/autodarts-browser

  grep -E -- '^--app=file:///nix/store/.*/loading.html$' "$out/splash-arguments"
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
