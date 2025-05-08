.PHONY: lexgen-types
lexgen-types:
	rm -rf ../atproto \
	&& rm -rf ./api/cbor/cbor_gen.go \
	&& git clone git@github.com:bluesky-social/atproto ../atproto \
	&& go run github.com/bluesky-social/indigo/cmd/lexgen \
		--build-file ./lexcfg.json \
		../atproto/lexicons \
		./lexicons/teal \
	&& go run ./util/gencbor/gencbor.go
