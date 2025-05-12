# piper

#### what is piper?

piper is a teal-fm tool that will be used to scrape user data from variety of
music providers.

#### why doesn't it work?

well its just a work in progress... we build in the open!

#### development

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

#### docker

TODO
