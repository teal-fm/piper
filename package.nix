{ lib, buildGoModule, tailwindcss_4, source ? ./. }:
buildGoModule {
  pname = "tealfm-piper";
  version = (builtins.fromJSON (builtins.readFile (source + "/package.json"))).version;

  src = source;

  vendorHash = "sha256-poQutY1V8X6BdmPMXdQuPWIWE/j3xNoEp4PKSimj2bA=";

  nativeBuildInputs = [ tailwindcss_4 ];

  env.CGO_ENABLED = 1;

  subPackages = [ "cmd" ];

  ldflags = [ "-s" "-w" ];

  postBuild = ''
    cp -r ./pages/templates $out/
    cp -r ./pages/static $out/
    tailwindcss -i $out/static/base.css -o $out/static/main.css -m
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
