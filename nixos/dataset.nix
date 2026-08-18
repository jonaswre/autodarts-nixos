{
  config,
  lib,
  pkgs,
  ...
}:
let
  cfg = config.services.autodarts-dataset;
  application = pkgs.buildGoModule {
    pname = "autodarts-dataset";
    version = "0.1.0";
    src = ../.;
    subPackages = [ "cmd/autodarts-dataset" ];
    vendorHash = "sha256-YuKp0vTjWchj4TuvPg9sR2BUKWIJVgqiwWn1vANHVqc=";
  };
in
{
  options.services.autodarts-dataset = {
    enable = lib.mkEnableOption "multi-camera Autodarts training-data recorder";
    port = lib.mkOption {
      type = lib.types.port;
      default = 8090;
      description = "Trusted-LAN dashboard port.";
    };
    quotaGB = lib.mkOption {
      type = lib.types.ints.positive;
      default = 20;
      description = "Maximum dataset storage in GiB.";
    };
    worldReferenceIntervalSeconds = lib.mkOption {
      type = lib.types.ints.positive;
      default = 300;
      description = "Seconds between stable physical-board world observations.";
    };
  };

  config = lib.mkIf cfg.enable {
    networking.firewall.allowedTCPPorts = [ cfg.port ];

    systemd.services.autodarts-dataset = {
      description = "Autodarts multi-camera training-data recorder";
      wantedBy = [ "multi-user.target" ];
      wants = [ "autodarts.service" ];
      after = [ "autodarts.service" ];
      serviceConfig = {
        DynamicUser = true;
        StateDirectory = "autodarts-dataset";
        StateDirectoryMode = "0700";
        WorkingDirectory = "/var/lib/autodarts-dataset";
        ExecStart = "${application}/bin/autodarts-dataset -url http://127.0.0.1:3180 -output /var/lib/autodarts-dataset/samples -quota-gb ${toString cfg.quotaGB} -world-reference-interval ${toString cfg.worldReferenceIntervalSeconds}s -listen 0.0.0.0:${toString cfg.port}";
        Restart = "always";
        RestartSec = "3s";
        NoNewPrivileges = true;
        PrivateTmp = true;
        ProtectHome = true;
        ProtectSystem = "strict";
        ReadWritePaths = [ "/var/lib/autodarts-dataset" ];
      };
    };
  };
}
