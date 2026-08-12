{
  config,
  lib,
  pkgs,
  ...
}:
let
  cfg = config.services.autodarts-onboarding;
  application = pkgs.buildGoModule {
    pname = "autodarts-onboarding";
    version = "0.1.0";
    src = ../.;
    subPackages = [ "cmd/autodarts-onboarding" ];
    vendorHash = "sha256-YuKp0vTjWchj4TuvPg9sR2BUKWIJVgqiwWn1vANHVqc=";
    postInstall = ''
      mkdir -p $out/share/autodarts-onboarding
    install -Dm644 ${../onboarding/device.html} $out/share/autodarts-onboarding/device.html
    install -Dm644 ${../onboarding/setup.html} $out/share/autodarts-onboarding/setup.html
    '';
  };
in
{
  options.services.autodarts-onboarding = {
    enable = lib.mkEnableOption "phone-based first-boot Autodarts setup" // {
      default = true;
    };
    port = lib.mkOption {
      type = lib.types.port;
      default = 3182;
      description = "Local-network port for the one-time phone setup portal.";
    };
  };

  config = lib.mkIf cfg.enable {
    users.groups.autodarts-onboarding = { };
    users.users.autodarts-onboarding = {
      isSystemUser = true;
      group = "autodarts-onboarding";
      home = "/var/lib/autodarts-onboarding";
    };

    networking.firewall.allowedTCPPorts = [ cfg.port ];

    systemd.services.autodarts-onboarding = {
      description = "Phone onboarding for the Autodarts appliance";
      wantedBy = [ "multi-user.target" ];
      after = [ "autodarts.service" ];
      wants = [ "autodarts.service" ];
      environment = {
        AUTODARTS_ONBOARDING_PORT = toString cfg.port;
        AUTODARTS_ONBOARDING_STATE = "/var/lib/autodarts-onboarding";
        AUTODARTS_ONBOARDING_ASSETS = "${application}/share/autodarts-onboarding";
      };
      serviceConfig = {
        User = "autodarts-onboarding";
        Group = "autodarts-onboarding";
        StateDirectory = "autodarts-onboarding";
        StateDirectoryMode = "0700";
        ExecStart = "${application}/bin/autodarts-onboarding";
        Restart = "on-failure";
        RestartSec = "2s";
        NoNewPrivileges = true;
        PrivateTmp = true;
        ProtectHome = true;
        ProtectSystem = "strict";
        ReadWritePaths = [ "/var/lib/autodarts-onboarding" ];
      };
    };
  };
}
