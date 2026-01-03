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

      mkPiper = pkgs:
        pkgs.buildGoModule {
          pname = "teal-piper";
          version = "0.0.2";
          src = ./.;
          vendorHash = "sha256-gYlVWk1TOUOB2J49smq9TyGw/6AQdyP/A6tzJsfe3kI=";

          nativeBuildInputs = [ pkgs.pkg-config ];
          buildInputs = [ pkgs.sqlite ];

          env.CGO_ENABLED = 1;
          subPackages = [ "cmd" ];
          ldflags = [ "-s" "-w" ];

          postInstall = ''
            mv $out/bin/cmd $out/bin/piper
          '';

          meta = with pkgs.lib; {
            description = "A teal.fm tool for scrobbling music to ATProto PDSs";
            homepage = "https://github.com/teal-fm/piper";
            license = licenses.mit;
            maintainers = [ ];
            mainProgram = "piper";
          };
        };
    in {
      packages = forAllSystems (system:
        let
          pkgs = nixpkgsFor.${system};
          piper = mkPiper pkgs;
        in {
          default = piper;
          teal-piper = piper;
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
          default = pkgs.mkShell { buildInputs = with pkgs; [ go air ]; };
        });

      nixosModules.default = import ./nixos-module.nix;
      nixosModules.teal-piper = import ./nixos-module.nix;

      overlays.default = final: prev: { teal-piper = mkPiper final; };
    };
}
