{ pkgs }:
let
  displayScale = import ../nixos/display-scale.nix { inherit pkgs; };
  choose = "${displayScale}/bin/autodarts-display-scale";
in
pkgs.runCommand "autodarts-display-scale-behavior" { } ''
  test "$(${choose} 1920x1080)" = 1
  test "$(${choose} 2560x1440)" = 1.5
  test "$(${choose} 3840x2160)" = 2
  test "$(${choose} 2160x3840)" = 2
  test "$(${choose} unknown)" = 1
  touch $out
''
