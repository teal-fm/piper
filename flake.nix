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
      nixpkgsFor = forAllSystems (system: import nixpkgs { inherit system; });
    in {
      packages = forAllSystems (system:
        let pkgs = nixpkgsFor.${system};
        in {
          tealfm-piper = pkgs.callPackage ./package.nix { };
          # Local development build 
          default = pkgs.callPackage ./package.nix { source = ./.; };
        });

      apps = forAllSystems (system:
        let piper = self.packages.${system}.default;
        in {
          default = {
            type = "app";
            program = "${piper}/bin/piper";
          };
        });

      devShells = forAllSystems (system:
        let pkgs = nixpkgsFor.${system};
        in {
          default =
            pkgs.mkShell { buildInputs = with pkgs; [ go air nodejs sqlite ]; };
        });

      nixosModules.default = { config, lib, pkgs, ... }:
        let
          piperPackage = self.packages.${pkgs.stdenv.hostPlatform.system}.tealfm-piper;
        in {
          imports = [
            (import ./module.nix { defaultPackage = piperPackage; })
          ];
        };

      overlays.default = final: prev: {
        tealfm-piper = final.callPackage ./package.nix { };
      };
    };
}
