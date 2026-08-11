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
    grep -F "Connecting to Autodarts" ${../kiosk/loading.html}
    grep -F "enable_auth=true" ${../nixos/kiosk.nix}
    grep -F "ConditionPathExists" ${../nixos/kiosk.nix}
    touch $out
  ''
