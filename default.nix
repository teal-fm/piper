{ lib, buildGoModule, sqlite }:

buildGoModule rec {
  pname = "tealfm-piper";
  version = "0.0.3";
  src = ./.;
  vendorHash = "sha256-poQutY1V8X6BdmPMXdQuPWIWE/j3xNoEp4PKSimj2bA=";
  buildInputs = [ sqlite ];
  env.CGO_ENABLED = 1;
  subPackages = [ "cmd" ];
  ldflags = [ "-s" "-w" ];

  postInstall = ''
    mv $out/bin/cmd $out/bin/piper
  '';

  meta = with lib; {
    description = "Music scrobbler service for teal.fm";
    longDescription = ''
      Piper is a teal.fm tool that scrobbles music plays from various
      music providers (Spotify, Apple Music, Last.fm) to ATProto Personal
      Data Servers using the teal.fm lexicons.
    '';
    homepage = "https://github.com/teal-fm/piper";
    changelog = "https://github.com/teal-fm/piper/releases/tag/v${version}";
    license = licenses.mit;
    maintainers = with maintainers; [ ptdewey ];
    mainProgram = "piper";
    platforms = lib.platforms.unix;
  };
}
