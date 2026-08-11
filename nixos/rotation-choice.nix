{ pkgs }:
pkgs.writeShellApplication {
  name = "autodarts-rotation-choice";
  text = ''
    case "''${1:-}" in
      "" | 1 | normal) echo normal ;;
      2 | 90) echo 90 ;;
      3 | 180) echo 180 ;;
      4 | 270) echo 270 ;;
      *)
        echo "Invalid display rotation choice." >&2
        exit 2
        ;;
    esac
  '';
}
