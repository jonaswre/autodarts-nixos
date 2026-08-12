{
  description = "Fast-booting Autodarts appliance for the Beelink Mini S12";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-26.05";
    disko = {
      url = "github:nix-community/disko";
      inputs.nixpkgs.follows = "nixpkgs";
    };
  };

  outputs =
    {
      self,
      nixpkgs,
      disko,
    }:
    let
      system = "x86_64-linux";
      mkSystem =
        disk: rotation:
        nixpkgs.lib.nixosSystem {
          inherit system;
          specialArgs = { inherit disk; };
          modules = [
            disko.nixosModules.disko
            ./nixos/disk.nix
            ./nixos/configuration.nix
            { services.autodarts-kiosk.rotation = nixpkgs.lib.mkForce rotation; }
          ];
        };
      targetSystems = {
        normal = mkSystem "/dev/nvme0n1" "normal";
        "90" = mkSystem "/dev/nvme0n1" "90";
        "180" = mkSystem "/dev/nvme0n1" "180";
        "270" = mkSystem "/dev/nvme0n1" "270";
      };
      # The installed system keeps the rotation selected by the installer in
      # configuration.nix. Only the pre-built installer targets force a value.
      beelinkSystem = nixpkgs.lib.nixosSystem {
        inherit system;
        specialArgs.disk = "/dev/nvme0n1";
        modules = [
          disko.nixosModules.disko
          ./nixos/disk.nix
          ./nixos/configuration.nix
        ];
      };
    in
    {
      nixosModules.default = ./nixos/kiosk.nix;
      nixosConfigurations.beelink = beelinkSystem;
      nixosConfigurations.installer = nixpkgs.lib.nixosSystem {
        inherit system;
        specialArgs = {
          inherit self disko;
          targetSystems = nixpkgs.lib.mapAttrs (
            _name: systemConfig: systemConfig.config.system.build.toplevel
          ) targetSystems;
        };
        modules = [ ./nixos/installer.nix ];
      };
      packages.${system} = {
        installer-iso = self.nixosConfigurations.installer.config.system.build.isoImage;
        default = self.packages.${system}.installer-iso;
      };
      checks.${system} = {
        nixos = self.nixosConfigurations.beelink.config.system.build.toplevel;
        kiosk = import ./tests/kiosk.nix { pkgs = nixpkgs.legacyPackages.${system}; };
        github-keys = import ./tests/github-keys.nix { pkgs = nixpkgs.legacyPackages.${system}; };
        onboarding = import ./tests/onboarding.nix { pkgs = nixpkgs.legacyPackages.${system}; };
        remote-control = import ./tests/remote-control.nix {
          pkgs = nixpkgs.legacyPackages.${system};
        };
        rotation-choice = import ./tests/rotation-choice.nix { pkgs = nixpkgs.legacyPackages.${system}; };
        display-scale = import ./tests/display-scale.nix { pkgs = nixpkgs.legacyPackages.${system}; };
        source = import ./tests/source.nix { pkgs = nixpkgs.legacyPackages.${system}; };
      };
      formatter.${system} = nixpkgs.legacyPackages.${system}.nixfmt;
    };
}
