{ pkgs }:
let
  githubKeys = import ../nixos/github-keys.nix { inherit pkgs; };
in
pkgs.runCommand "autodarts-github-key-lookup" { nativeBuildInputs = [ pkgs.netcat-openbsd ]; } ''
  body=$'ssh-ed25519 AAAAC3Nza-test first-device\nssh-rsa AAAAB3Nza-test fallback-device\n'
  (printf 'HTTP/1.1 200 OK\r\nContent-Length: %s\r\nConnection: close\r\n\r\n%s' "''${#body}" "$body" | nc -l 127.0.0.1 18081) &

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
