{
  description = "Piper - A scrobbler service for teal.fm";
  inputs = { nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable"; };

  outputs = { self, nixpkgs }:
    let
      forAllSystems = nixpkgs.lib.genAttrs [
        "x86_64-linux"
        "aarch64-linux"
        "x86_64-darwin"
        "aarch64-darwin"
      ];
    in {
      packages = forAllSystems (system:
        let pkgs = import nixpkgs { inherit system; };
        in {
          tealfm-piper = pkgs.callPackage ./package.nix { };
          default = pkgs.callPackage ./package.nix { source = ./.; };
        });

      apps = forAllSystems (system: {
        default = {
          type = "app";
          program = "${self.packages.${system}.default}/bin/piper";
        };
      });

      devShells = forAllSystems (system:
        let pkgs = import nixpkgs { inherit system; };
        in {
          default =
            pkgs.mkShell { buildInputs = with pkgs; [ go air nodejs sqlite ]; };
        });

      nixosModules.default = import ./module.nix { inherit self; };

      overlays.default = final: prev: {
        tealfm-piper = final.callPackage ./package.nix { };
      };
    };
}
