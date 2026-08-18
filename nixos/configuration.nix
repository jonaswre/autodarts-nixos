{ lib, pkgs, ... }:
{
  imports = [
    ./autodarts.nix
    ./dataset.nix
    ./kiosk.nix
  ]
  ++ lib.optional (builtins.pathExists ../device-local.nix) ../device-local.nix;

  # Change to "normal", "180", or "270" for a different mounting direction.
  services.autodarts-kiosk.rotation = lib.mkDefault "normal";

  networking = {
    hostName = "autodarts";
    networkmanager.enable = true;
    firewall.allowedTCPPorts = [
      22
      3180
    ];
  };

  # Alder Lake-N graphics, Wi-Fi and the three UVC cameras benefit from current
  # kernel and firmware. The kernel also supplies i915 hardware acceleration.
  boot = {
    kernelPackages = pkgs.linuxPackages_latest;
    initrd.systemd.enable = true;
    initrd.verbose = false;
    consoleLogLevel = 0;
    kernelParams = [
      "quiet"
      "loglevel=3"
      "udev.log_level=3"
      "i915.fastboot=1"
    ];
    loader = {
      timeout = 0;
      systemd-boot.enable = true;
      efi.canTouchEfiVariables = true;
    };
  };
  hardware = {
    enableRedistributableFirmware = true;
    graphics.enable = true;
  };
  services.fstrim.enable = true;
  services.xserver.enable = false;

  # Keep boot off the network critical path. Services needing connectivity wait
  # independently; the kiosk can immediately show Chromium's offline screen.
  systemd.network.wait-online.enable = false;
  systemd.services.NetworkManager-wait-online.enable = false;

  services.openssh = {
    enable = true;
    settings = {
      PasswordAuthentication = false;
      KbdInteractiveAuthentication = false;
      PermitRootLogin = "prohibit-password";
    };
  };

  users.users.admin = {
    uid = 1000;
    isNormalUser = true;
    extraGroups = [
      "wheel"
      "networkmanager"
    ];
    openssh.authorizedKeys.keys = [ ];
  };
  # Physical console access remains possible without shipping a shared password.
  services.getty.autologinUser = "admin";
  security.sudo.wheelNeedsPassword = false;

  environment.systemPackages = with pkgs; [
    curl
    git
    htop
    networkmanager
  ];
  time.timeZone = "Europe/Berlin";
  i18n.defaultLocale = "en_US.UTF-8";

  system.stateVersion = "26.05";
}
