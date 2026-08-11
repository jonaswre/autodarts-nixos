{ pkgs }:
let
  rotationChoice = import ../nixos/rotation-choice.nix { inherit pkgs; };
  choose = "${rotationChoice}/bin/autodarts-rotation-choice";
in
pkgs.runCommand "autodarts-rotation-choice-behavior" { } ''
  [[ "$(${choose} "")" == normal ]]
  [[ "$(${choose} 1)" == normal ]]
  [[ "$(${choose} normal)" == normal ]]
  [[ "$(${choose} 2)" == 90 ]]
  [[ "$(${choose} 90)" == 90 ]]
  [[ "$(${choose} 3)" == 180 ]]
  [[ "$(${choose} 180)" == 180 ]]
  [[ "$(${choose} 4)" == 270 ]]
  [[ "$(${choose} 270)" == 270 ]]

  if ${choose} sideways 2>/dev/null; then
    echo "Invalid rotation was accepted" >&2
    exit 1
  fi

  touch "$out"
''
