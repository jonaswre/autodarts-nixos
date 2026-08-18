{ pkgs, lib }:
let
  evaluated = lib.nixosSystem {
    system = pkgs.stdenv.hostPlatform.system;
    modules = [
      ../nixos/dataset.nix
      {
        system.stateVersion = "26.05";
        services.autodarts-dataset.enable = true;
      }
    ];
  };
  service = evaluated.config.systemd.services.autodarts-dataset;
in
assert builtins.elem 8090 evaluated.config.networking.firewall.allowedTCPPorts;
assert service.serviceConfig.StateDirectory == "autodarts-dataset";
pkgs.runCommand "autodarts-dataset-service-check" { } ''
  command=${lib.escapeShellArg service.serviceConfig.ExecStart}
  printf '%s\n' "$command" | grep -F '0.0.0.0:8090'
  printf '%s\n' "$command" | grep -F -- '-quota-gb 20'
  printf '%s\n' "$command" | grep -F -- '-world-reference-interval 300s'
  touch "$out"
''
