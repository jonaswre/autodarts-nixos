{ pkgs }:
let
  githubKeys = import ../nixos/github-keys.nix { inherit pkgs; };
in
pkgs.runCommand "autodarts-github-key-lookup" { nativeBuildInputs = [ pkgs.python3 ]; } ''
  ${pkgs.python3}/bin/python - <<'PY' &
  from http.server import BaseHTTPRequestHandler, HTTPServer

  class GitHubKeys(BaseHTTPRequestHandler):
      def do_GET(self):
          assert self.path == "/dart-player.keys"
          body = (
              "ssh-ed25519 AAAAC3Nza-test first-device\n"
              "ssh-rsa AAAAB3Nza-test fallback-device\n"
          ).encode()
          self.send_response(200)
          self.send_header("Content-Length", str(len(body)))
          self.end_headers()
          self.wfile.write(body)

      def log_message(self, *_args):
          pass

  server = HTTPServer(("127.0.0.1", 18081), GitHubKeys)
  server.timeout = 10
  server.handle_request()
  PY

  server_pid=$!
  GITHUB_KEYS_BASE_URL=http://127.0.0.1:18081 \
    ${githubKeys}/bin/autodarts-github-keys dart-player > keys
  wait "$server_pid"

  grep -Fx 'ssh-ed25519 AAAAC3Nza-test first-device' keys
  grep -Fx 'ssh-rsa AAAAB3Nza-test fallback-device' keys
  [[ $(wc -l < keys) -eq 2 ]]

  if ${githubKeys}/bin/autodarts-github-keys 'invalid/user' 2>/dev/null; then
    echo "Invalid GitHub username was accepted" >&2
    exit 1
  fi

  touch "$out"
''
