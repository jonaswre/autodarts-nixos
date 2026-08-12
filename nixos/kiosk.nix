{
  config,
  lib,
  pkgs,
  ...
}:
let
  browser = import ./kiosk-launcher.nix { inherit pkgs; };
  cfg = config.services.autodarts-kiosk;
  vncStateDir = "/var/lib/autodarts-vnc";
  vncSetup = pkgs.writeShellApplication {
    name = "autodarts-vnc-setup";
    runtimeInputs = [
      pkgs.coreutils
      pkgs.openssl
      pkgs.systemd
    ];
    text = ''
            if [[ $EUID -ne 0 ]]; then
              echo "Run this command with sudo." >&2
              exit 1
            fi

            install -d -m 0710 -o autodarts-vnc -g kiosk ${vncStateDir}
            temp_dir=$(mktemp -d ${vncStateDir}/setup.XXXXXX)
            trap 'rm -rf "$temp_dir"' EXIT

            openssl req -x509 -newkey rsa:3072 -nodes -sha256 -days 3650 \
              -subj "/CN=autodarts" \
              -keyout "$temp_dir/tls-key.pem" \
              -out "$temp_dir/tls-cert.pem" >/dev/null 2>&1

            cat > "$temp_dir/config" <<EOF
      address=127.0.0.1
      port=${toString cfg.vnc.port}
      enable_auth=false
      EOF

            chown -R autodarts-vnc:kiosk "$temp_dir"
            chmod 0640 "$temp_dir/config" "$temp_dir/tls-key.pem"
            chmod 0644 "$temp_dir/tls-cert.pem"
            mv -f "$temp_dir/config" "$temp_dir/tls-key.pem" "$temp_dir/tls-cert.pem" ${vncStateDir}/
            rm -f ${vncStateDir}/web-credentials
            rmdir "$temp_dir"
            trap - EXIT

            if systemctl is-active --quiet autodarts-kiosk.service; then
              systemctl restart autodarts-wayvnc.service autodarts-novnc.service
            fi
            echo "Browser remote control is ready at https://autodarts:${toString cfg.vnc.webPort}/vnc.html"
            openssl x509 -in ${vncStateDir}/tls-cert.pem -noout -fingerprint -sha256
    '';
  };
in
{
  imports = [ ./onboarding.nix ];

  options.services.autodarts-kiosk = {
    enable = lib.mkEnableOption "the Autodarts Chromium kiosk" // {
      default = true;
    };
    rotation = lib.mkOption {
      type = lib.types.enum [
        "normal"
        "90"
        "180"
        "270"
      ];
      default = "normal";
      description = "Display transform. Use 90 for a monitor mounted clockwise.";
    };
    vnc = {
      enable = lib.mkEnableOption "encrypted WayVNC remote control" // {
        default = true;
      };
      port = lib.mkOption {
        type = lib.types.port;
        default = 5900;
        description = "TCP port for encrypted VNC remote control.";
      };
      webPort = lib.mkOption {
        type = lib.types.port;
        default = 6080;
        description = "HTTPS port for browser-based noVNC remote control.";
      };
    };
  };

  config = lib.mkIf cfg.enable {
    users.groups.kiosk = { };
    users.users.kiosk = {
      isSystemUser = true;
      group = "kiosk";
      extraGroups = [
        "video"
        "input"
        "seat"
        "audio"
      ];
      home = "/var/lib/autodarts-kiosk";
      createHome = true;
    };
    users.groups.autodarts-vnc = { };
    users.users.autodarts-vnc = {
      isSystemUser = true;
      group = "autodarts-vnc";
      extraGroups = [
        "kiosk"
        "video"
        "input"
      ];
      home = vncStateDir;
    };

    services.seatd = {
      enable = true;
      group = "seat";
    };

    environment.systemPackages = lib.mkIf cfg.vnc.enable [ vncSetup ];
    networking.firewall.allowedTCPPorts = lib.mkIf cfg.vnc.enable [ cfg.vnc.webPort ];

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
        AUTODARTS_ONBOARDING_URL = "http://127.0.0.1:${toString config.services.autodarts-onboarding.port}";
        AUTODARTS_ONBOARDING_ENABLED = lib.boolToString config.services.autodarts-onboarding.enable;
      };
      serviceConfig = {
        User = "kiosk";
        Group = "kiosk";
        SupplementaryGroups = [
          "video"
          "input"
          "seat"
          "audio"
        ];
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

    systemd.services.autodarts-wayvnc = lib.mkIf cfg.vnc.enable {
      description = "Encrypted remote control for the Autodarts kiosk";
      wantedBy = [ "multi-user.target" ];
      after = [ "autodarts-kiosk.service" ];
      unitConfig.ConditionPathExists = "${vncStateDir}/config";
      environment = {
        XDG_RUNTIME_DIR = "/run/autodarts-kiosk";
        WAYLAND_DISPLAY = "wayland-0";
      };
      serviceConfig = {
        User = "kiosk";
        Group = "kiosk";
        SupplementaryGroups = [
          "kiosk"
          "video"
          "input"
        ];
        StateDirectory = "autodarts-vnc";
        StateDirectoryMode = "0700";
        ExecStartPre = pkgs.writeShellScript "wait-for-autodarts-wayland" ''
          for _ in {1..40}; do
            [[ -S /run/autodarts-kiosk/wayland-0 ]] && exit 0
            ${pkgs.coreutils}/bin/sleep 0.25
          done
          echo "Timed out waiting for the kiosk Wayland socket" >&2
          exit 1
        '';
        ExecStart = "${pkgs.wayvnc}/bin/wayvnc --config ${vncStateDir}/config";
        # A monitor hotplug makes WayVNC exit successfully when its selected
        # wlroots output disappears. Restart so it follows the replacement
        # connector after TVs/monitors finish their cold-boot HDMI handshake.
        Restart = "always";
        RestartSec = "2s";
      };
    };

    systemd.services.autodarts-novnc = lib.mkIf cfg.vnc.enable {
      description = "HTTPS browser remote control for the Autodarts kiosk";
      wantedBy = [ "multi-user.target" ];
      requires = [ "autodarts-wayvnc.service" ];
      after = [ "autodarts-wayvnc.service" ];
      unitConfig.ConditionPathExists = "${vncStateDir}/config";
      serviceConfig = {
        User = "kiosk";
        Group = "kiosk";
        ExecStart = "${pkgs.python3Packages.websockify}/bin/websockify --ssl-only --cert=${vncStateDir}/tls-cert.pem --key=${vncStateDir}/tls-key.pem --web=${pkgs.novnc}/share/webapps/novnc 0.0.0.0:${toString cfg.vnc.webPort} 127.0.0.1:${toString cfg.vnc.port}";
        Restart = "on-failure";
        RestartSec = "2s";
      };
    };
  };
}
