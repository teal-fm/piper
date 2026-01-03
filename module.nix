{ config, lib, pkgs, ... }:

let
  cfg = config.services.teal-piper;

  # Helper function to generate environment variables
  settingsFormat = pkgs.formats.keyValue { };

  # Auto-derive URLs from SERVER_ROOT_URL if not explicitly set
  defaultSettings = {
    SERVER_PORT = cfg.settings.SERVER_PORT or 8080;
    SERVER_HOST = cfg.settings.SERVER_HOST or "localhost";
    DB_PATH = cfg.settings.DB_PATH or "${cfg.dataDir}/piper.db";
    TRACKER_INTERVAL = cfg.settings.TRACKER_INTERVAL or 30;

    # Spotify defaults
    SPOTIFY_AUTH_URL =
      cfg.settings.SPOTIFY_AUTH_URL or "https://accounts.spotify.com/authorize";
    SPOTIFY_TOKEN_URL =
      cfg.settings.SPOTIFY_TOKEN_URL or "https://accounts.spotify.com/api/token";
    SPOTIFY_SCOPES =
      cfg.settings.SPOTIFY_SCOPES or "user-read-currently-playing user-read-email";
  };

  # Auto-derive callback URLs if SERVER_ROOT_URL is set
  derivedSettings = lib.optionalAttrs (cfg.settings ? SERVER_ROOT_URL) {
    ATPROTO_CLIENT_ID =
      cfg.settings.ATPROTO_CLIENT_ID or "${cfg.settings.SERVER_ROOT_URL}/oauth-client-metadata.json";
    ATPROTO_METADATA_URL =
      cfg.settings.ATPROTO_METADATA_URL or "${cfg.settings.SERVER_ROOT_URL}/oauth-client-metadata.json";
    ATPROTO_CALLBACK_URL =
      cfg.settings.ATPROTO_CALLBACK_URL or "${cfg.settings.SERVER_ROOT_URL}/callback/atproto";
    CALLBACK_SPOTIFY =
      cfg.settings.CALLBACK_SPOTIFY or "${cfg.settings.SERVER_ROOT_URL}/callback/spotify";
  };

  # Merge user settings with defaults and derived settings
  finalSettings = defaultSettings // cfg.settings // derivedSettings;

  # Create environment file
  settingsFile = settingsFormat.generate "teal-piper.env" finalSettings;

in {
  meta = {
    maintainers = with lib.maintainers;
      [ ]; # Add maintainer info when upstreaming
  };

  options.services.teal-piper = {
    enable = lib.mkEnableOption "Piper - teal.fm scrobbler service";

    package = lib.mkPackageOption pkgs "teal-piper" { };

    user = lib.mkOption {
      type = lib.types.str;
      default = "teal-piper";
      description = "User account under which piper runs.";
    };

    group = lib.mkOption {
      type = lib.types.str;
      default = "teal-piper";
      description = "Group under which piper runs.";
    };

    dataDir = lib.mkOption {
      type = lib.types.path;
      default = "/var/lib/teal-piper";
      description = "Directory where piper stores its database and data.";
    };

    settings = lib.mkOption {
      type = lib.types.attrsOf lib.types.str;
      default = { };
      example = lib.literalExpression ''
        {
          SERVER_PORT = "8080";
          SERVER_HOST = "0.0.0.0";
          SERVER_ROOT_URL = "https://piper.teal.fm";
          TRACKER_INTERVAL = "30";
        }
      '';
      description = ''
        Configuration for piper. These will be converted to environment variables.

        Required settings (set via environmentFile for security):
        - SERVER_ROOT_URL: Public URL for OAuth callbacks
        - SPOTIFY_CLIENT_ID: From Spotify Developer Dashboard
        - SPOTIFY_CLIENT_SECRET: From Spotify Developer Dashboard
        - ATPROTO_CLIENT_SECRET_KEY: P-256 private key (generate with: goat key generate -t P-256)
        - ATPROTO_CLIENT_SECRET_KEY_ID: Unique persistent identifier

        Optional settings:
        - SERVER_PORT: Port to listen on (default: 8080)
        - SERVER_HOST: Host to bind to (default: localhost)
        - TRACKER_INTERVAL: Seconds between music checks (default: 30)
        - LASTFM_API_KEY: For Last.fm integration
        - APPLE_MUSIC_TEAM_ID: For Apple Music integration
        - APPLE_MUSIC_KEY_ID: For Apple Music integration
        - APPLE_MUSIC_PRIVATE_KEY_PATH: Path to Apple Music private key

        URLs are auto-derived from SERVER_ROOT_URL:
        - ATPROTO_CLIENT_ID
        - ATPROTO_METADATA_URL
        - ATPROTO_CALLBACK_URL
        - CALLBACK_SPOTIFY
      '';
    };

    environmentFile = lib.mkOption {
      type = lib.types.nullOr lib.types.path;
      default = null;
      example = "/run/secrets/teal-piper.env";
      description = ''
        Path to a file containing environment variables for secrets.
        ```
        SPOTIFY_CLIENT_ID=your_spotify_client_id
        SPOTIFY_CLIENT_SECRET=your_spotify_client_secret
        ATPROTO_CLIENT_SECRET_KEY=your_p256_private_key
        ATPROTO_CLIENT_SECRET_KEY_ID=1758199756
        LASTFM_API_KEY=your_lastfm_key
        ```
      '';
    };
  };

  config = lib.mkIf cfg.enable {
    users.users.${cfg.user} = {
      isSystemUser = true;
      group = cfg.group;
      home = cfg.dataDir;
      description = "Piper service user";
    };

    users.groups.${cfg.group} = { };

    systemd.services.teal-piper = {
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
        StateDirectory = "teal-piper";
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
          "services.teal-piper: ATPROTO_CLIENT_SECRET_KEY must be set via settings or environmentFile";
      }
      {
        assertion = cfg.environmentFile != null
          || (cfg.settings ? ATPROTO_CLIENT_SECRET_KEY_ID);
        message =
          "services.teal-piper: ATPROTO_CLIENT_SECRET_KEY_ID must be set via settings or environmentFile";
      }
      {
        assertion = cfg.settings ? SERVER_ROOT_URL;
        message =
          "services.teal-piper: SERVER_ROOT_URL must be set in settings (e.g., https://piper.teal.fm)";
      }
    ];
  };
}
