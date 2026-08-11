{
  config,
  lib,
  pkgs,
  modulesPath,
  self,
  disko,
  targetSystem,
  ...
}:
let
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
      echo "Optional: paste one SSH public key for the admin user, or press Enter to skip."
      read -r ssh_key
      if [[ -n "$ssh_key" && ! "$ssh_key" =~ ^(ssh-ed25519|ssh-rsa|ecdsa-sha2-nistp(256|384|521))[[:space:]] ]]; then
        echo "Refusing invalid SSH public key." >&2
        exit 1
      fi

      disko --mode disko ${source}/nixos/disk.nix --argstr disk "$target"

      install -d /mnt/etc/nixos
      cp -a ${source}/. /mnt/etc/nixos/
      nixos-install --no-root-password --system ${targetSystem}

      if [[ -n "$ssh_key" ]]; then
        install -d -m 0700 -o 1000 -g 100 /mnt/home/admin/.ssh
        printf '%s\n' "$ssh_key" > /mnt/home/admin/.ssh/authorized_keys
        chown 1000:100 /mnt/home/admin/.ssh/authorized_keys
        chmod 0600 /mnt/home/admin/.ssh/authorized_keys
      fi

      echo
      echo "Installation complete. Remove the USB drive, then run: reboot"
    '';
  };
in
{
  imports = [
    (modulesPath + "/installer/cd-dvd/installation-cd-minimal.nix")
  ];

  isoImage = {
    volumeID = lib.mkForce "AUTODARTS";
    storeContents = [ targetSystem ];
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
