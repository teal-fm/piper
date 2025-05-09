.PHONY: dev-setup
dev-setup:
	rm -rf ../atproto \
	&& git clone git@github.com:bluesky-social/atproto ../atproto \

.PHONY: go-lexicons
go-lexicons:
	rm -rf ./api/cbor/cbor_gen.go \
	&& rm -rf ./api/teal \
	&& mkdir -p ./api/teal \
	&& $(MAKE) lexgen \
	&& sed -i .bak 's/\tutil/\/\/\tutil/' $$(find ./api/teal -type f) \
	&& go run golang.org/x/tools/cmd/goimports@latest -w $$(find ./api/teal -type f) \
	&& go run ./util/gencbor/gencbor.go \
	&& $(MAKE) lexgen \
	&& find . | grep bak$$ | xargs rm 

.PHONY: lexgen
lexgen:
	$(MAKE) lexgen-types

.PHONY: lexgen-types
lexgen-types:
	go run github.com/bluesky-social/indigo/cmd/lexgen \
		--build-file ./lexcfg.json \
		../atproto/lexicons \
		./lexicons/teal \
