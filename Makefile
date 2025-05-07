.PHONY: lexgen-types
lexgen-types:
	rm -rf ../atproto \
	&& git clone git@github.com:bluesky-social/atproto \
	&& mv atproto ../
	go run github.com/bluesky-social/indigo/cmd/lexgen \
		--build-file ./lexcfg.json \
		../atproto/lexicons \
		./lexicons/teal
