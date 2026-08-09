{
  description = "Fast-booting Autodarts appliance for the Beelink Mini S12";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-26.05";
    disko = {
      url = "github:nix-community/disko";
      inputs.nixpkgs.follows = "nixpkgs";
    };
  };

  outputs = { self, nixpkgs, disko }:
    let
      system = "x86_64-linux";
      mkSystem = disk: nixpkgs.lib.nixosSystem {
        inherit system;
        specialArgs = { inherit disk; };
        modules = [
          disko.nixosModules.disko
          ./nixos/disk.nix
          ./nixos/configuration.nix
        ];
      };
      beelinkSystem = mkSystem "/dev/nvme0n1";
    in {
      nixosModules.default = ./nixos/kiosk.nix;
      nixosConfigurations.beelink = beelinkSystem;
      nixosConfigurations.installer = nixpkgs.lib.nixosSystem {
        inherit system;
        specialArgs = {
          inherit self disko;
          targetSystem = beelinkSystem.config.system.build.toplevel;
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
        source = import ./tests/source.nix { pkgs = nixpkgs.legacyPackages.${system}; };
      };
      formatter.${system} = nixpkgs.legacyPackages.${system}.nixfmt-rfc-style;
    };
}
