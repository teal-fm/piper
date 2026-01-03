{ system ? builtins.currentSystem, pkgs ? import <nixpkgs> { inherit system; }
}:
let
  flake = import ./flake.nix;
  outputs = flake.outputs {
    self = outputs;
    nixpkgs = pkgs.path or <nixpkgs>;
  };
in outputs.packages.${system}.default
