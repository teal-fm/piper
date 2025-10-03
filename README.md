# piper

#### what is piper?

piper is a teal-fm tool that will be used to scrape user data from variety of
music providers.

#### why doesn't it work?

well its just a work in progress... we build in the open!

## setup
It is recommend to have port forward url while working with piper. Development or running from docker because of external callbacks.

You have a couple of options
1. Setup the traditional port forward on your router
2. Use a tool like [ngrok](https://ngrok.com/) with the command `ngrok http 8080` or [Cloudflare tunnels](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/get-started/create-remote-tunnel/) (follow the 2a. portion of the guide when you get to that point)

Either way make note of what the publicly accessible domain name is for setting up env variables. It will be something like `https://piper.teal.fm` that you can access publicly

#### env variables
Copy [.env.template](.env.template) and name it [.env](.env)

This is a break down of what each env variable is and what it may look like

- `SERVER_PORT` - The port piper is hosted on
- `SERVER_HOST` - The server host. `localhost` is fine here, or `0.0.0.0` for docker
- `SERVER_ROOT_URL` - This needs to be the pubically accessible url created in [Setup](#setup). Like `https://piper.teal.fm`
- `SPOTIFY_CLIENT_ID` - Client Id from setup in [Spotify developer dashboard](https://developer.spotify.com/documentation/web-api/tutorials/getting-started)
- `SPOTIFY_CLIENT_SECRET` - Client Secret from setup in [Spotify developer dashboard](https://developer.spotify.com/documentation/web-api/tutorials/getting-started)
- `SPOTIFY_AUTH_URL` - most likely `https://accounts.spotify.com/authorize`
- `SPOTIFY_TOKEN_URL` - most likely `https://accounts.spotify.com/api/token`
- `SPOTIFY_SCOPES` - most likely `user-read-currently-playing user-read-email`
- `CALLBACK_SPOTIFY` - The first part is your publicly accessible domain. So will something like this `https://piper.teal.fm/callback/spotify`

- `ATPROTO_CLIENT_ID` - The first part is your publicly accessible domain. So will something like this `https://piper.teal.fm/.well-known/client-metadata.json`
- `ATPROTO_METADATA_URL` - The first part is your publicly accessible domain. So will something like this `https://piper.teal.fm/.well-known/client-metadata.json`
- `ATPROTO_CALLBACK_URL` - The first part is your publicly accessible domain. So will something like this `https://piper.teal.fm/callback/atproto`

- `LASTFM_API_KEY` - Your lastfm api key. Can find out how to setup [here](https://www.last.fm/api)

- `TRACKER_INTERVAL` - How long between checks to see if the registered users are listening to new music
- `DB_PATH`= Path for the sqlite db. If you are using the docker compose probably want `/db/piper.db` to persist data



## development

make sure you have your env setup following [the env var setup](#env-variables)

assuming you have go installed and set up properly:

run some make scripts:

```
make jwtgen

make dev-setup
```

install air:

```
go install github.com/air-verse/air@latest
```

run air:

```
air
```
air should automatically build and run piper, and watch for changes on relevant files.


## tailwindcss

To use tailwindcss you will have to install the tailwindcss cli. This will take the [./pages/static/base.css](./pages/static/base.css) and transform it into a [./pages/static/main.css](./pages/static/main.css) 
which is imported on the [./pages/templates/layouts/base.gohtml](./pages/templates/layouts/base.gohtml). When running the dev server tailwindcss will watch for changes and recompile the main.css file.

1. Install tailwindcss cli `npm install tailwindcss @tailwindcss/cli`
2. run `npx @tailwindcss/cli -i ./pages/static/base.css -o ./pages/static/main.css --watch`




#### Lexicon changes
1. Copy the new or changed json schema files to the [lexicon folders](./lexicons)
2. run `make go-lexicons`

Go types should be updated and should have the changes to the schemas 

#### docker
We also provide a docker compose file to use to run piper locally. There are a few edits to the [.env](.env) to make it run smoother in a container
`SERVER_HOST`- `0.0.0.0`
`DB_PATH` = `/db/piper.db` to persist your piper db through container restarts

Make sure you have docker and docker compose installed, then you can run piper with `docker compose up`
