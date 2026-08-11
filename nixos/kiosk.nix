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

            username="''${1:-autodarts}"
            read -r -s -p "VNC password (minimum 8 characters): " password
            echo
            read -r -s -p "Repeat VNC password: " confirmation
            echo
            [[ "$password" == "$confirmation" ]] || { echo "Passwords do not match." >&2; exit 1; }
            [[ ''${#password} -ge 8 ]] || { echo "Password is too short." >&2; exit 1; }
            [[ "$password" != *$'\n'* && "$password" != *$'\r'* ]] || {
              echo "Password contains an invalid newline." >&2
              exit 1
            }

            install -d -m 0700 -o autodarts-vnc -g autodarts-vnc ${vncStateDir}
            temp_dir=$(mktemp -d ${vncStateDir}/setup.XXXXXX)
            trap 'rm -rf "$temp_dir"' EXIT

            openssl req -x509 -newkey rsa:3072 -nodes -sha256 -days 3650 \
              -subj "/CN=autodarts" \
              -keyout "$temp_dir/tls-key.pem" \
              -out "$temp_dir/tls-cert.pem" >/dev/null 2>&1

            cat > "$temp_dir/config" <<EOF
      address=0.0.0.0
      port=${toString cfg.vnc.port}
      enable_auth=true
      username=$username
      password=$password
      private_key_file=${vncStateDir}/tls-key.pem
      certificate_file=${vncStateDir}/tls-cert.pem
      EOF

            chown -R autodarts-vnc:autodarts-vnc "$temp_dir"
            chmod 0600 "$temp_dir/config" "$temp_dir/tls-key.pem"
            chmod 0644 "$temp_dir/tls-cert.pem"
            mv -f "$temp_dir/config" "$temp_dir/tls-key.pem" "$temp_dir/tls-cert.pem" ${vncStateDir}/
            rmdir "$temp_dir"
            trap - EXIT

            systemctl restart autodarts-wayvnc.service
            echo "Encrypted VNC is ready on port ${toString cfg.vnc.port} for user: $username"
            openssl x509 -in ${vncStateDir}/tls-cert.pem -noout -fingerprint -sha256
    '';
  };
in
{
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
    networking.firewall.allowedTCPPorts = lib.mkIf cfg.vnc.enable [ cfg.vnc.port ];

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
        User = "autodarts-vnc";
        Group = "autodarts-vnc";
        SupplementaryGroups = [
          "kiosk"
          "video"
          "input"
        ];
        StateDirectory = "autodarts-vnc";
        StateDirectoryMode = "0700";
        ExecStart = "${pkgs.wayvnc}/bin/wayvnc --config ${vncStateDir}/config";
        Restart = "on-failure";
        RestartSec = "2s";
      };
    };
  };
}
