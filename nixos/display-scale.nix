{ pkgs }:
pkgs.writeShellApplication {
  name = "autodarts-display-scale";
  text = ''
    mode="''${1:-}"
    width="''${mode%%x*}"
    height="''${mode#*x}"
    height="''${height%%[^0-9]*}"

    if [[ ! "$width" =~ ^[0-9]+$ || ! "$height" =~ ^[0-9]+$ ]]; then
      echo 1
    elif (( width >= 3200 || height >= 3200 )); then
      echo 2
    elif (( width >= 2400 || height >= 2400 )); then
      echo 1.5
    else
      echo 1
    fi
  '';
}
