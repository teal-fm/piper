{ lib, buildGoModule, tailwindcss_4, source ? ./. }:
buildGoModule {
  pname = "tealfm-piper";
  version = (builtins.fromJSON (builtins.readFile (source + "/package.json"))).version;

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
