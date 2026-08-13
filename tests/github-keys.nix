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

    cat > fake-curl <<'EOF'
  #!/bin/sh
  printf '%s\n' "$*" >> "$GITHUB_KEYS_CURL_LOG"
  case " $* " in
    *" --ipv4 "*)
      printf '%s\n' 'ssh-ed25519 AAAAC3Nza-ipv4-test ipv4-fallback'
      ;;
    *)
      exit 7
      ;;
  esac
  EOF
    chmod +x fake-curl

    GITHUB_KEYS_CURL="$PWD/fake-curl" \
      GITHUB_KEYS_CURL_LOG="$PWD/curl.log" \
      GITHUB_KEYS_RETRIES=0 \
      ${githubKeys}/bin/autodarts-github-keys dart-player > ipv4-keys 2> ipv4-errors

    grep -Fx 'ssh-ed25519 AAAAC3Nza-ipv4-test ipv4-fallback' ipv4-keys
    [[ $(wc -l < curl.log) -eq 2 ]]
    sed -n '1p' curl.log | grep -Fv -- '--ipv4'
    sed -n '2p' curl.log | grep -F -- '--ipv4'
    grep -F 'retrying this request over IPv4' ipv4-errors

    grep -F -- '--retry-all-errors' ${../nixos/github-keys.nix}
    grep -F 'Press Enter to retry' ${../nixos/installer.nix}

    if ${githubKeys}/bin/autodarts-github-keys 'invalid/user' 2>/dev/null; then
      echo "Invalid GitHub username was accepted" >&2
      exit 1
    fi

    touch "$out"
''
