{
  config,
  lib,
  pkgs,
  modulesPath,
  self,
  disko,
  targetSystems,
  ...
}:
let
  githubKeys = import ./github-keys.nix { inherit pkgs; };
  rotationChoice = import ./rotation-choice.nix { inherit pkgs; };
  source = builtins.path {
    path = ../.;
    name = "autodarts-nixos-source";
    filter =
      path: type:
      let
        base = baseNameOf path;
      in
      base != ".git" && !(lib.hasPrefix "result" base) && !(lib.hasSuffix ".iso" base);
  };

  installer = pkgs.writeShellApplication {
    name = "autodarts-install";
    runtimeInputs = [
      pkgs.coreutils
      pkgs.gawk
      pkgs.gnugrep
      pkgs.gnused
      pkgs.util-linux
      disko.packages.${pkgs.stdenv.hostPlatform.system}.disko
      config.system.build.nixos-install
    ];
    text = ''
      if [[ $# -gt 0 ]]; then
        target="$1"
      else
        mapfile -t internal_disks < <(
          lsblk -dnpo NAME,TYPE,RM | awk '$2 == "disk" && $3 == "0" { print $1 }'
        )
        if [[ ''${#internal_disks[@]} -ne 1 ]]; then
          echo "Could not identify exactly one non-removable internal disk." >&2
          echo "Run autodarts-install /dev/DEVICE explicitly after checking lsblk." >&2
          exit 1
        fi
        target="''${internal_disks[0]}"
      fi
      target_name="$(basename "$target")"

      echo
      echo "Autodarts appliance installer"
      echo "=============================="
      lsblk -o NAME,SIZE,MODEL,TRAN,TYPE,MOUNTPOINTS
      echo

      if [[ ! -b "$target" ]]; then
        echo "Refusing to continue: $target does not exist." >&2
        exit 1
      fi
      if [[ ! -e "/sys/class/block/$target_name/removable" ||
            "$(cat "/sys/class/block/$target_name/removable")" != 0 ]]; then
        echo "Refusing to continue: $target is marked removable." >&2
        exit 1
      fi
      if lsblk -nrpo MOUNTPOINTS "$target" | grep -q '[^[:space:]]'; then
        echo "Refusing to overwrite a mounted target." >&2
        exit 1
      fi

      echo "This permanently erases the internal drive $target."
      echo "Type exactly: ERASE $target"
      read -r confirmation
      [[ "$confirmation" == "ERASE $target" ]] || {
        echo "Cancelled. Run autodarts-install to try again."
        exit 1
      }

      echo
      echo "Optional: enter a GitHub username to install its public SSH keys."
      echo "Press Enter to skip remote administration."
      read -r github_user
      ssh_keys=""
      if [[ -n "$github_user" ]]; then
        echo "Downloading public keys from https://github.com/$github_user.keys"
        ssh_keys="$(${githubKeys}/bin/autodarts-github-keys "$github_user")"
      fi

      echo
      echo "Display orientation:"
      echo "  1) Landscape (normal)"
      echo "  2) Portrait, monitor rotated clockwise (90 degrees)"
      echo "  3) Landscape, upside down (180 degrees)"
      echo "  4) Portrait, monitor rotated counter-clockwise (270 degrees)"
      echo "Choose 1-4, or press Enter for landscape:"
      read -r rotation_input
      rotation="$(${rotationChoice}/bin/autodarts-rotation-choice "$rotation_input")"

      case "$rotation" in
        normal) target_system=${targetSystems.normal} ;;
        90) target_system=${targetSystems."90"} ;;
        180) target_system=${targetSystems."180"} ;;
        270) target_system=${targetSystems."270"} ;;
      esac

      disko --mode disko ${source}/nixos/disk.nix --argstr disk "$target"

      install -d /mnt/etc/nixos
      cp -a ${source}/. /mnt/etc/nixos/
      sed -i \
        "s/services.autodarts-kiosk.rotation = \"normal\";/services.autodarts-kiosk.rotation = \"$rotation\";/" \
        /mnt/etc/nixos/nixos/configuration.nix
      nixos-install --no-root-password --system "$target_system"

      nixos-enter --root /mnt -c "autodarts-vnc-setup"

      if [[ -n "$ssh_keys" ]]; then
        install -d -m 0700 -o 1000 -g 100 /mnt/home/admin/.ssh
        printf '%s\n' "$ssh_keys" > /mnt/home/admin/.ssh/authorized_keys
        chown 1000:100 /mnt/home/admin/.ssh/authorized_keys
        chmod 0600 /mnt/home/admin/.ssh/authorized_keys
      fi

      echo
      echo "Installation complete. Remove the USB drive, then run: reboot"
      echo "After boot, open https://autodarts:6080/vnc.html"
    '';
  };
in
{
  imports = [
    (modulesPath + "/installer/cd-dvd/installation-cd-minimal.nix")
  ];

  isoImage = {
    volumeID = lib.mkForce "AUTODARTS";
    storeContents = builtins.attrValues targetSystems;
    squashfsCompression = "zstd -Xcompression-level 6 -processors 4";
  };
  image.fileName = lib.mkForce "autodarts-beelink-installer.iso";

  boot.kernelPackages = pkgs.linuxPackages_latest;
  boot.supportedFilesystems = lib.mkForce [
    "vfat"
    "ext4"
    "iso9660"
  ];
  hardware.enableRedistributableFirmware = true;
  networking.networkmanager.enable = true;
  services.openssh.enable = true;

  environment.systemPackages = [ installer ];
  services.getty.autologinUser = lib.mkForce "root";
  programs.bash.interactiveShellInit = lib.mkAfter ''
    if [[ "$(tty 2>/dev/null || true)" == /dev/tty1 && -z "''${AUTODARTS_INSTALLER_SHOWN:-}" ]]; then
      export AUTODARTS_INSTALLER_SHOWN=1
      ${installer}/bin/autodarts-install
    fi
  '';

  system.stateVersion = "26.05";
}
