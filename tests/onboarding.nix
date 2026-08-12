{ pkgs }:
let
  application = pkgs.buildGoModule {
    pname = "autodarts-onboarding-test-app";
    version = "0.1.0";
    src = ../.;
    subPackages = [ "cmd/autodarts-onboarding" ];
    vendorHash = "sha256-imsXbwHohEy9hQqnpPa2NBIK9YGk2lY09cM4hDWFFcI=";
  };
in
pkgs.runCommand "autodarts-onboarding-behavior" { nativeBuildInputs = [ pkgs.python3 ]; } ''
  python ${./onboarding.py} ${application}/bin/autodarts-onboarding ${../onboarding}
  touch $out
''
