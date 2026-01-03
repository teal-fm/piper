{
  description = "Piper - A teal.fm scrobbler service for ATProto";
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
          default = pkgs.callPackage ./default.nix { };
          teal-piper = pkgs.callPackage ./default.nix { };
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

      nixosModules.default = import ./module.nix;
      nixosModules.teal-piper = import ./module.nix;

      overlays.default = final: prev: {
        teal-piper = final.callPackage ./default.nix { };
      };
    };
}
