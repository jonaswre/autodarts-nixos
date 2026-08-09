{ config, lib, pkgs, ... }:
let
  browser = import ./kiosk-launcher.nix { inherit pkgs; };
  cfg = config.services.autodarts-kiosk;
in
{
  options.services.autodarts-kiosk = {
    enable = lib.mkEnableOption "the Autodarts Chromium kiosk" // { default = true; };
    rotation = lib.mkOption {
      type = lib.types.enum [ "normal" "90" "180" "270" ];
      default = "normal";
      description = "Display transform. Use 90 for a monitor mounted clockwise.";
    };
  };

  config = lib.mkIf cfg.enable {
  users.groups.kiosk = { };
  users.users.kiosk = {
    isSystemUser = true;
    group = "kiosk";
    extraGroups = [ "video" "input" "seat" "audio" ];
    home = "/var/lib/autodarts-kiosk";
    createHome = true;
  };

  services.seatd = {
    enable = true;
    group = "seat";
  };

  systemd.services.autodarts-kiosk = {
    description = "Autodarts WebClient kiosk when a display is connected";
    wantedBy = [ "multi-user.target" ];
    after = [ "seatd.service" ];
    wants = [ "seatd.service" ];
    environment = {
      XDG_RUNTIME_DIR = "/run/autodarts-kiosk";
      XDG_SESSION_TYPE = "wayland";
      LIBSEAT_BACKEND = "seatd";
      AUTODARTS_ROTATION = cfg.rotation;
    };
    serviceConfig = {
      User = "kiosk";
      Group = "kiosk";
      SupplementaryGroups = [ "video" "input" "seat" "audio" ];
      RuntimeDirectory = "autodarts-kiosk";
      StateDirectory = "autodarts-kiosk";
      WorkingDirectory = "/var/lib/autodarts-kiosk";
      ExecStartPre = "+${pkgs.kbd}/bin/chvt 7";
      ExecStart = "${pkgs.cage}/bin/cage -- ${browser}/bin/autodarts-browser";
      Restart = "on-failure";
      RestartSec = "2s";
      TTYPath = "/dev/tty7";
      StandardInput = "tty";
      StandardOutput = "journal";
    };
  };
  };
}
