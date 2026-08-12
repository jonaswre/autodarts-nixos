{ pkgs }:
pkgs.runCommand "autodarts-source-checks"
  {
    nativeBuildInputs = [
      pkgs.jq
      pkgs.shellcheck
    ];
  }
  ''
    shellcheck ${../install.sh} ${../scripts/flash-installer.sh}
    jq -e '.manifest_version == 3' ${../kiosk/extension/manifest.json} >/dev/null
    jq -e '.content_scripts[0].matches | index("https://play.autodarts.io/*")' \
      ${../kiosk/extension/manifest.json} >/dev/null
    jq -e '.content_scripts[0].matches | index("http://127.0.0.1:3180/*")' \
      ${../kiosk/extension/manifest.json} >/dev/null
    grep -F "location.href = onBoardManager" ${../kiosk/extension/controls.js}
    grep -F "https://play.autodarts.io/" ${../kiosk/extension/controls.js}
    grep -F "http://127.0.0.1:3180/" ${../kiosk/extension/controls.js}
    grep -F "Remote control QR code" ${../kiosk/extension/controls.js}
    grep -F "remote-control-qr.svg" ${../kiosk/extension/background.js}
    grep -F "Connecting to Autodarts" ${../kiosk/loading.html}
    grep -F "enable_auth=false" ${../nixos/kiosk.nix}
    grep -F "address=127.0.0.1" ${../nixos/kiosk.nix}
    grep -F "Timed out waiting for the kiosk Wayland socket" ${../nixos/kiosk.nix}
    grep -F 'Restart = "always"' ${../nixos/kiosk.nix}
    grep -F '"$randr" --output "$connector" --scale "$scale"' ${../nixos/kiosk-launcher.nix}
    grep -F 'The installed system keeps the rotation selected by the installer' ${../flake.nix}
    grep -F "autodarts-novnc" ${../nixos/kiosk.nix}
    grep -F -- "--ssl-only" ${../nixos/kiosk.nix}
    grep -F 'nixos-enter --root /mnt -c "autodarts-vnc-setup"' ${../nixos/installer.nix}
    grep -F "ConditionPathExists" ${../nixos/kiosk.nix}
    grep -F "https://github.com/\$github_user.keys" ${../nixos/installer.nix}
    grep -F "Portrait, monitor rotated clockwise" ${../nixos/installer.nix}
    grep -F "Pairing details are only shown on the appliance display" ${../onboarding/server.py}
    grep -F 'id="board-id"' ${../onboarding/setup.html}
    grep -F 'id="api-key"' ${../onboarding/setup.html}
    touch $out
  ''
