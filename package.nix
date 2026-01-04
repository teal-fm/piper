{ lib, buildGoModule, tailwindcss_4, fetchFromGitHub, source ? fetchFromGitHub {
  owner = "teal-fm";
  repo = "piper";
  rev = "ccb72442021bd9f6ed20acc63f9703cf475b0f51";
  hash = "sha256-wXA2RnvQ0J0QwUeDIg2gLRI2DNjgu07+QYjw5pRmyyI=";
} }:
buildGoModule {
  pname = "tealfm-piper";
  version = "0.0.3";

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
