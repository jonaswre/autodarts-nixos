{ pkgs }:
pkgs.buildGoModule {
  pname = "autodarts-remote-control-behavior";
  version = "0.1.0";
  src = ../.;
  subPackages = [ "cmd/autodarts-novnc" ];
  vendorHash = "sha256-YuKp0vTjWchj4TuvPg9sR2BUKWIJVgqiwWn1vANHVqc=";
  doCheck = true;
}
