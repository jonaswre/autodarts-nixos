{ pkgs }:
pkgs.writeShellApplication {
  name = "autodarts-github-keys";
  runtimeInputs = [ pkgs.curl ];
  text = ''
    if [[ $# -ne 1 || ! "$1" =~ ^[A-Za-z0-9][A-Za-z0-9-]{0,38}$ || "$1" == *- || "$1" == *--* ]]; then
      echo "Invalid GitHub username." >&2
      exit 2
    fi

    username="$1"
    base_url="''${GITHUB_KEYS_BASE_URL:-https://github.com}"
    response=$(curl --fail --silent --show-error --max-time 15 \
      --retry 2 --retry-connrefused --retry-delay 0 \
      "$base_url/$username.keys") || {
      echo "Could not download SSH keys for GitHub user: $username" >&2
      exit 1
    }

    valid_keys=()
    while IFS= read -r key; do
      key="''${key%$'\r'}"
      [[ -z "$key" ]] && continue
      if [[ ! "$key" =~ ^(ssh-ed25519|ssh-rsa|ecdsa-sha2-nistp(256|384|521))[[:space:]] ]]; then
        echo "GitHub returned an unsupported SSH key." >&2
        exit 1
      fi
      valid_keys+=("$key")
    done <<< "$response"

    if [[ ''${#valid_keys[@]} -eq 0 ]]; then
      echo "GitHub user $username has no supported public SSH keys." >&2
      exit 1
    fi

    printf '%s\n' "''${valid_keys[@]}"
  '';
}
