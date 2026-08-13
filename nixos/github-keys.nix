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
    curl_bin="''${GITHUB_KEYS_CURL:-curl}"

    download_keys() {
      "$curl_bin" --fail --silent --show-error \
        --connect-timeout "''${GITHUB_KEYS_CONNECT_TIMEOUT:-10}" \
        --max-time "''${GITHUB_KEYS_MAX_TIME:-45}" \
        --retry "''${GITHUB_KEYS_RETRIES:-4}" \
        --retry-all-errors \
        --retry-delay "''${GITHUB_KEYS_RETRY_DELAY:-2}" \
        "$@" "$base_url/$username.keys"
    }

    if ! response="$(download_keys)"; then
      echo "Normal GitHub connection failed; retrying this request over IPv4." >&2
      response="$(download_keys --ipv4)" || {
        echo "Could not download SSH keys for GitHub user: $username" >&2
        exit 1
      }
    fi

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
