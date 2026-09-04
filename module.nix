{ self ? null }:
{ config, lib, pkgs, ... }:

let
  inherit (lib) mkEnableOption mkIf mkOption types literalExpression;

  cfg = config.services.tealfm-piper;

  settingsFormat = pkgs.formats.keyValue { };

  settingOr = name: fallback:
    let value = cfg.settings.${name} or null;
    in if value != null then value else fallback;

  derivedSettings = lib.optionalAttrs (cfg.settings.SERVER_ROOT_URL != null) {
    ATPROTO_CLIENT_ID = settingOr "ATPROTO_CLIENT_ID"
      "${cfg.settings.SERVER_ROOT_URL}/oauth-client-metadata.json";
    ATPROTO_CALLBACK_URL = settingOr "ATPROTO_CALLBACK_URL"
      "${cfg.settings.SERVER_ROOT_URL}/callback/atproto";
    CALLBACK_SPOTIFY = settingOr "CALLBACK_SPOTIFY"
      "${cfg.settings.SERVER_ROOT_URL}/callback/spotify";
  };

  dbPathDefault = lib.optionalAttrs (cfg.settings.DB_PATH == null) {
    DB_PATH = "${cfg.dataDir}/piper.db";
  };

  allowedDidsString = lib.optionalAttrs (cfg.settings.ALLOWED_DIDS != null) {
    ALLOWED_DIDS = lib.concatStringsSep " " cfg.settings.ALLOWED_DIDS;
  };

  finalSettings = lib.filterAttrs (_: v: v != null)
    (cfg.settings // derivedSettings // dbPathDefault // allowedDidsString);
  settingsFile = settingsFormat.generate "tealfm-piper.env" finalSettings;

in {
  meta = { maintainers = with lib.maintainers; [ ptdewey ]; };

  options.services.tealfm-piper = {
    enable = mkEnableOption "Piper - teal.fm scrobbler service";

    package = mkOption {
      type = types.package;
      default = if self != null then
        self.packages.${pkgs.stdenv.hostPlatform.system}.tealfm-piper
      else
        pkgs.tealfm-piper;
      defaultText = literalExpression "pkgs.tealfm-piper";
      description = "The piper package to use.";
    };

    user = mkOption {
      type = types.str;
      default = "tealfm-piper";
      description = "User account under which piper runs.";
    };

    group = mkOption {
      type = types.str;
      default = "tealfm-piper";
      description = "Group under which piper runs.";
    };

    dataDir = mkOption {
      type = types.path;
      default = "/var/lib/tealfm-piper";
      description = "Directory where piper stores its database and data.";
    };

    settings = mkOption {
      type = types.submodule {
        freeformType = types.attrsOf
          (types.nullOr (types.oneOf [ types.bool types.int types.str ]));

        options = {
          SERVER_PORT = mkOption {
            type = types.port;
            default = 8080;
            description = "Port to listen on.";
          };

          SERVER_HOST = mkOption {
            type = types.str;
            default = "localhost";
            description = "Host to bind to.";
          };

          SERVER_ROOT_URL = mkOption {
            type = types.nullOr types.str;
            default = null;
            example = "https://piper.teal.fm";
            description = ''
              Public URL for OAuth callbacks. Required for OAuth flows.

              Auto-derives the following URLs if not explicitly set:
              - ATPROTO_CLIENT_ID
              - ATPROTO_CALLBACK_URL
              - CALLBACK_SPOTIFY
            '';
          };

          ENABLE_SPOTIFY = mkOption {
            type = types.bool;
            default = true;
            description = "Whether to enable Spotify integration.";
          };

          ENABLE_LASTFM = mkOption {
            type = types.bool;
            default = true;
            description = "Whether to enable Last.fm integration.";
          };

          ENABLE_APPLEMUSIC = mkOption {
            type = types.bool;
            default = true;
            description = "Whether to enable Apple Music integration.";
          };

          DB_PATH = mkOption {
            type = types.nullOr types.str;
            default = null;
            description = ''
              Path to SQLite database file.
              Defaults to {dataDir}/piper.db if not set.
            '';
          };

          TRACKER_INTERVAL = mkOption {
            type = types.ints.positive;
            default = 30;
            description = "Seconds between Spotify and Apple Music playback checks.";
          };

          LASTFM_INTERVAL_SECONDS = mkOption {
            type = types.ints.positive;
            default = 30;
            description = "Seconds between Last.fm playback checks.";
          };

          SPOTIFY_AUTH_URL = mkOption {
            type = types.str;
            default = "https://accounts.spotify.com/authorize";
            description = "Spotify authorization endpoint.";
          };

          SPOTIFY_TOKEN_URL = mkOption {
            type = types.str;
            default = "https://accounts.spotify.com/api/token";
            description = "Spotify token endpoint.";
          };

          SPOTIFY_SCOPES = mkOption {
            type = types.str;
            default = "user-read-currently-playing user-read-email";
            description = "Spotify OAuth scopes to request.";
          };

          ATPROTO_CLIENT_ID = mkOption {
            type = types.nullOr types.str;
            default = null;
            description = "ATProto OAuth client ID. Derived from SERVER_ROOT_URL by default.";
          };

          ATPROTO_CALLBACK_URL = mkOption {
            type = types.nullOr types.str;
            default = null;
            description = "ATProto OAuth callback URL. Derived from SERVER_ROOT_URL by default.";
          };

          CALLBACK_SPOTIFY = mkOption {
            type = types.nullOr types.str;
            default = null;
            description = "Spotify OAuth callback URL. Derived from SERVER_ROOT_URL by default.";
          };

          ALLOWED_DIDS = mkOption {
            type = types.nullOr (types.listOf types.str);
            default = null;
            example =
              literalExpression ''[ "did:plc:abcdefg" "did:web:example.com" ]'';
            description = ''
              List of ATProto DIDs allowed to sign in.
              When set, restricts instance access to only these accounts.
              Leave null to allow any ATProto account to sign in.
            '';
          };
        };
      };

      default = { };

      example = literalExpression ''
        {
          SERVER_PORT = 8080;
          SERVER_HOST = "localhost";
          SERVER_ROOT_URL = "https://piper.teal.fm";
          ENABLE_APPLEMUSIC = false;
          TRACKER_INTERVAL = 30;
        }
      '';

      description = ''
        Configuration for piper. These will be converted to environment variables.
        Do not put secrets here because the generated file is stored in the Nix store.
        Use environmentFiles for credentials and private keys.
      '';
    };

    environmentFiles = mkOption {
      type = types.listOf types.path;
      default = [ ];
      example = literalExpression ''
        [
          "/run/secrets/tealfm-piper.env"
          "/run/secrets/tealfm-piper-apple-music.env"
        ]
      '';
      description = ''
        List of files containing environment variables for secrets.
        Files are loaded in order, with later files overriding earlier ones.
      '';
    };
  };

  config = mkIf cfg.enable {
    users.users.${cfg.user} = {
      isSystemUser = true;
      group = cfg.group;
      home = cfg.dataDir;
      description = "Piper service user";
    };

    users.groups.${cfg.group} = { };

    systemd.tmpfiles.rules = [
      "d '${cfg.dataDir}' 0700 ${cfg.user} ${cfg.group} - -"
    ];

    systemd.services.tealfm-piper = {
      description = "Piper - teal.fm scrobbler service";
      after = [ "network-online.target" ];
      wants = [ "network-online.target" ];
      wantedBy = [ "multi-user.target" ];

      serviceConfig = {
        Type = "simple";
        User = cfg.user;
        Group = cfg.group;
        NoNewPrivileges = true;
        PrivateTmp = true;
        PrivateDevices = true;
        ProtectSystem = "strict";
        ProtectHome = true;
        ProtectKernelTunables = true;
        ProtectKernelModules = true;
        ProtectControlGroups = true;
        RestrictAddressFamilies = [ "AF_INET" "AF_INET6" "AF_UNIX" ];
        RestrictNamespaces = true;
        RestrictRealtime = true;
        RestrictSUIDSGID = true;
        LockPersonality = true;
        ReadWritePaths = [ cfg.dataDir ];
        WorkingDirectory = cfg.dataDir;
        EnvironmentFile = [ settingsFile ] ++ cfg.environmentFiles;
        ExecStart = "${cfg.package}/bin/piper";
        Restart = "on-failure";
        RestartSec = "10s";
      };
    };

    assertions = [
      {
        assertion = (cfg.environmentFiles != [ ])
          || ((cfg.settings.ATPROTO_CLIENT_SECRET_KEY or null) != null
            && cfg.settings.ATPROTO_CLIENT_SECRET_KEY != "");
        message =
          "services.tealfm-piper: ATPROTO_CLIENT_SECRET_KEY must be set via settings or environmentFiles";
      }
      {
        assertion = (cfg.environmentFiles != [ ])
          || ((cfg.settings.ATPROTO_CLIENT_SECRET_KEY_ID or null) != null
            && cfg.settings.ATPROTO_CLIENT_SECRET_KEY_ID != "");
        message =
          "services.tealfm-piper: ATPROTO_CLIENT_SECRET_KEY_ID must be set via settings or environmentFiles";
      }
      {
        assertion = cfg.settings.SERVER_ROOT_URL != null
          && cfg.settings.SERVER_ROOT_URL != "";
        message =
          "services.tealfm-piper: SERVER_ROOT_URL must be set in settings (e.g., https://piper.teal.fm)";
      }
    ];
  };
}
