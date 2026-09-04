{ lib, buildGoModule, tailwindcss_4, fetchFromGitHub, source ? fetchFromGitHub {
  owner = "teal-fm";
  repo = "piper";
  rev = "6ff8c772debd067d2255f254780d22486d99b07f";
  hash = "sha256-n1G6nbz3Lt38RxP9nwCRLvx31WOO8Au15geyiAWKcAo=";
} }:
buildGoModule {
  pname = "tealfm-piper";
  version = "0.0.11";

  src = source;

  vendorHash = "sha256-0CAKzBBARoHSqDv34Xx3Yek6r33Exhrhvn+FzGlby14=";

  nativeBuildInputs = [ tailwindcss_4 ];

  env.CGO_ENABLED = 1;

  subPackages = [ "cmd" ];

  ldflags = [
    "-s"
    "-w"
    "-X main.buildTime=2026-09-02T13:18:08-05:00"
  ];

  preBuild = ''
    tailwindcss -i ./pages/static/base.css -o ./pages/static/main.css -m
  '';

  postInstall = ''
    mv $out/bin/cmd $out/bin/piper
  '';

  meta = with lib; {
    description = "Music scrobbler service for teal.fm";
    homepage = "https://github.com/teal-fm/piper";
    license = licenses.mit;
    maintainers = with maintainers; [ ptdewey ];
    mainProgram = "piper";
  };
}
