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

#### Lexicon changes
1. Copy the new or changed json schema files to the [lexicon folders](./lexicons)
2. run `make go-lexicons`

Go types should be updated and should have the changes to the schemas 

#### docker

TODO


