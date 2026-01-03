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
          default = pkgs.callPackage ./package.nix { };
          teal-piper = pkgs.callPackage ./package.nix { };
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
          default = pkgs.mkShell {
            buildInputs = with pkgs; [ go air nodejs sqlite pkg-config ];
          };
        });

      nixosModules.default = import ./module.nix;
      nixosModules.teal-piper = import ./module.nix;

      overlays.default = final: prev: {
        teal-piper = final.callPackage ./package.nix { };
      };
    };
}
