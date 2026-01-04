{ config, lib, pkgs, ... }:

let
  inherit (lib)
    mkEnableOption mkIf mkOption mkPackageOption types literalExpression;

  cfg = config.services.tealfm-piper;

  settingsFormat = pkgs.formats.keyValue { };

  # Auto-derive callback URLs if SERVER_ROOT_URL is set
  derivedSettings = lib.optionalAttrs (cfg.settings.SERVER_ROOT_URL != null) {
    ATPROTO_CLIENT_ID =
      cfg.settings.ATPROTO_CLIENT_ID or "${cfg.settings.SERVER_ROOT_URL}/oauth-client-metadata.json";
    ATPROTO_METADATA_URL =
      cfg.settings.ATPROTO_METADATA_URL or "${cfg.settings.SERVER_ROOT_URL}/oauth-client-metadata.json";
    ATPROTO_CALLBACK_URL =
      cfg.settings.ATPROTO_CALLBACK_URL or "${cfg.settings.SERVER_ROOT_URL}/callback/atproto";
    CALLBACK_SPOTIFY =
      cfg.settings.CALLBACK_SPOTIFY or "${cfg.settings.SERVER_ROOT_URL}/callback/spotify";
  };

  dbPathDefault = lib.optionalAttrs (cfg.settings.DB_PATH == null) {
    DB_PATH = "${cfg.dataDir}/piper.db";
  };

  finalSettings = lib.filterAttrs (_: v: v != null)
    (cfg.settings // derivedSettings // dbPathDefault);
  settingsFile = settingsFormat.generate "tealfm-piper.env" finalSettings;

in {
  meta = { maintainers = with lib.maintainers; [ ptdewey ]; };

  options.services.tealfm-piper = {
    enable = mkEnableOption "Piper - teal.fm scrobbler service";

    package = mkPackageOption pkgs "tealfm-piper" { };

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
          (types.oneOf [ (types.nullOr types.str) types.int types.port ]);

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
              - ATPROTO_METADATA_URL
              - ATPROTO_CALLBACK_URL
              - CALLBACK_SPOTIFY
            '';
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
            type = types.int;
            default = 30;
            description = "Seconds between music playback checks.";
          };

          # Spotify defaults
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
        };
      };

      default = { };

      example = literalExpression ''
        {
          SERVER_PORT = 8080;
          SERVER_HOST = "localhost";
          SERVER_ROOT_URL = "https://piper.teal.fm";
          TRACKER_INTERVAL = 30;
        }
      '';

      description = ''
        Configuration for piper. These will be converted to environment variables.
      '';
    };

    # TODO: maybe change to `environmentFiles`
    environmentFile = mkOption {
      type = types.nullOr types.path;
      default = null;
      example = "/run/secrets/tealfm-piper.env";
      description = ''
        Path to a file containing environment variables for secrets.

        Example content:
        ```
        SPOTIFY_CLIENT_ID=your_spotify_client_id
        SPOTIFY_CLIENT_SECRET=your_spotify_client_secret
        ATPROTO_CLIENT_SECRET_KEY=your_p256_private_key
        ATPROTO_CLIENT_SECRET_KEY_ID=1234567890
        LASTFM_API_KEY=your_lastfm_key
        ```
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

    systemd.services.tealfm-piper = {
      description = "Piper - teal.fm scrobbler service";
      after = [ "network-online.target" ];
      wants = [ "network-online.target" ];
      wantedBy = [ "multi-user.target" ];

      serviceConfig = {
        Type = "simple";
        User = cfg.user;
        Group = cfg.group;

        # Security hardening
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

        # Allow write access to data directory
        ReadWritePaths = [ cfg.dataDir ];
        StateDirectory = "tealfm-piper";
        StateDirectoryMode = "0700";

        # Working directory
        WorkingDirectory = cfg.dataDir;

        # Load environment from generated file
        EnvironmentFile = [ settingsFile ]
          ++ lib.optional (cfg.environmentFile != null) cfg.environmentFile;

        # Start the service
        ExecStart = "${cfg.package}/bin/piper";

        # Restart policy
        Restart = "on-failure";
        RestartSec = "10s";
      };
    };

    assertions = [
      {
        assertion = cfg.environmentFile != null
          || (cfg.settings ? ATPROTO_CLIENT_SECRET_KEY);
        message =
          "services.tealfm-piper: ATPROTO_CLIENT_SECRET_KEY must be set via settings or environmentFile";
      }
      {
        assertion = cfg.environmentFile != null
          || (cfg.settings ? ATPROTO_CLIENT_SECRET_KEY_ID);
        message =
          "services.tealfm-piper: ATPROTO_CLIENT_SECRET_KEY_ID must be set via settings or environmentFile";
      }
      {
        assertion = cfg.settings.SERVER_ROOT_URL != null;
        message =
          "services.tealfm-piper: SERVER_ROOT_URL must be set in settings (e.g., https://piper.teal.fm)";
      }
    ];
  };
}
