{ pkgs, lib }:
let
  evaluated = lib.nixosSystem {
    system = pkgs.stdenv.hostPlatform.system;
    modules = [
      ../nixos/kiosk.nix
      { services.autodarts-kiosk.rotation = lib.mkDefault "normal"; }
      ./fixtures/device-local-270.nix
      { system.stateVersion = "26.05"; }
    ];
  };
  service = evaluated.config.systemd.services.autodarts-kiosk;
in
assert service.environment.AUTODARTS_ROTATION == "270";
pkgs.runCommand "autodarts-rotation-persistence-check" { } ''
  touch "$out"
''
