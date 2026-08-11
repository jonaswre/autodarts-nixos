{ pkgs }:
let
  python = pkgs.python3.withPackages (packages: [ packages.qrcode ]);
  application = pkgs.runCommand "autodarts-onboarding-test-app" { } ''
    mkdir -p $out
    cp ${../onboarding/server.py} $out/server.py
    cp ${../onboarding/device.html} $out/device.html
    cp ${../onboarding/setup.html} $out/setup.html
  '';
in
pkgs.runCommand "autodarts-onboarding-behavior" { nativeBuildInputs = [ python ]; } ''
  python ${./onboarding.py} ${application}
  touch $out
''
