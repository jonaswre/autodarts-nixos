{ pkgs }:
pkgs.runCommand "autodarts-remote-control-behavior"
  {
    nativeBuildInputs = [ pkgs.python3 ];
  }
  ''
    python ${./remote-control.py} \
      ${pkgs.python3Packages.websockify}/bin/websockify \
      ${pkgs.novnc}/share/webapps/novnc \
      ${pkgs.openssl}/bin/openssl
    touch $out
  ''
