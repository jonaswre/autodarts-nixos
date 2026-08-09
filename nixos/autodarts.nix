{ lib, pkgs, ... }:
let
  autodarts = pkgs.stdenv.mkDerivation {
    pname = "autodarts-detection";
    version = "1.0.7";
    src = pkgs.fetchurl {
      url = "https://get.autodarts.io/detection/latest/linux/amd64/autodarts1.0.7.linux-amd64.tar.gz";
      hash = "sha256-udE0vBGIK6MdKpCugzU/HuDkUVROfKk36C6sWzp0a6A=";
    };
    nativeBuildInputs = [ pkgs.autoPatchelfHook ];
    sourceRoot = ".";
    installPhase = ''
      runHook preInstall
      install -Dm755 autodarts $out/bin/autodarts
      runHook postInstall
    '';
    meta.mainProgram = "autodarts";
  };
in
{
  users.groups.autodarts = { };
  users.users.autodarts = {
    isSystemUser = true;
    group = "autodarts";
    extraGroups = [ "video" ];
    home = "/var/lib/autodarts";
    createHome = true;
  };

  services.udev.extraRules = ''
    SUBSYSTEM=="video4linux", GROUP="video", MODE="0660"
  '';

  systemd.services.autodarts = {
    description = "Autodarts board detection";
    wantedBy = [ "multi-user.target" ];
    wants = [ "network-online.target" ];
    after = [ "network-online.target" ];
    serviceConfig = {
      User = "autodarts";
      Group = "autodarts";
      SupplementaryGroups = [ "video" ];
      WorkingDirectory = "/var/lib/autodarts";
      StateDirectory = "autodarts";
      ExecStart = lib.getExe autodarts;
      Restart = "on-failure";
      RestartSec = "2s";
      KillSignal = "SIGINT";
      NoNewPrivileges = true;
      PrivateTmp = true;
      ProtectSystem = "strict";
      ProtectHome = true;
      ReadWritePaths = [ "/var/lib/autodarts" ];
    };
  };
}
