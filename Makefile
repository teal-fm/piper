.PHONY: lexgen-types
lexgen-types:
	go run github.com/bluesky-social/indigo/cmd/lexgen \
		--build-file ./lexcfg.json \
		../atproto/lexicons \
		./lexicons/teal
