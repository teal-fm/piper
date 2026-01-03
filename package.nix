{ lib, buildGoModule, pkg-config, sqlite }:

buildGoModule rec {
  pname = "teal-piper";
  version = "0.0.2";

  src = ./.;

  vendorHash = "sha256-gYlVWk1TOUOB2J49smq9TyGw/6AQdyP/A6tzJsfe3kI=";

  nativeBuildInputs = [ pkg-config ];
  buildInputs = [ sqlite ];

  # CGO is required for sqlite3 support
  env.CGO_ENABLED = 1;

  # Only build the cmd package
  subPackages = [ "cmd" ];

  # Strip debug symbols for smaller binary size
  ldflags = [ "-s" "-w" ];

  # Rename binary from 'cmd' to 'piper' for better UX
  postInstall = ''
    mv $out/bin/cmd $out/bin/piper
  '';

  meta = {
    description = "Music scrobbler service for ATProto";
    longDescription = ''
      Piper is a teal.fm tool that scrobbles music plays from various
      music providers (Spotify, Apple Music, Last.fm) to ATProto Personal
      Data Servers using the teal.fm lexicons.

      It runs as a web service that periodically checks configured music
      services for currently playing tracks and submits them to your
      ATProto PDS for social music listening features.
    '';
    homepage = "https://github.com/teal-fm/piper";
    changelog = "https://github.com/teal-fm/piper/releases/tag/v${version}";
    license = lib.licenses.mit;
    maintainers = [ ];
    mainProgram = "piper";
    platforms = lib.platforms.unix;
  };
}
