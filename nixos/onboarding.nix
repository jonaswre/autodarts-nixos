{
  config,
  lib,
  pkgs,
  ...
}:
let
  cfg = config.services.autodarts-onboarding;
  python = pkgs.python3.withPackages (packages: [ packages.qrcode ]);
  application = pkgs.runCommand "autodarts-onboarding" { } ''
    mkdir -p $out/share/autodarts-onboarding
    install -Dm755 ${../onboarding/server.py} $out/share/autodarts-onboarding/server.py
    install -Dm644 ${../onboarding/device.html} $out/share/autodarts-onboarding/device.html
    install -Dm644 ${../onboarding/setup.html} $out/share/autodarts-onboarding/setup.html
  '';
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
      };
      serviceConfig = {
        User = "autodarts-onboarding";
        Group = "autodarts-onboarding";
        StateDirectory = "autodarts-onboarding";
        StateDirectoryMode = "0700";
        ExecStart = "${python}/bin/python ${application}/share/autodarts-onboarding/server.py";
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
