.PHONY: go-lexicons
go-lexicons:
	# Clean up the existing output directory
	rm -rf ./pkg/teal
	# Recreate the output directory
	mkdir -p ./pkg/teal
	# Remove potentially leftover generated files (adjust if your generator creates different specific files)
	rm -rf ./pkg/teal/cbor_gen.go
	# Run the lexicon generator
	$(MAKE) lexgen
	# Apply sed transformations if needed (adjust the pattern 's/\tutil/\/\/\tutil/' if necessary)
	sed -i.bak 's/\tutil/\/\/\tutil/' $$(find ./pkg/ -type f)
	# Format the generated Go files
	go run golang.org/x/tools/cmd/goimports@latest -w $$(find ./pkg/teal -type f)
	# Remove backup files created by sed
	rm -rf ./pkg/teal/*.bak

.PHONY: lexgen
lexgen:
	# Run the lexgen command with your project's configuration
	./lexgen --package $(package) \
		--types-import $(prefix):$(import) \
		-outdir $(outdir) \
		--prefix $(prefix) \
		--build-file lexcfg.json \
		$(lexicon_dir) \
	    $(atproto_lex_dir)

# Define variables from your configuration
package := teal
prefix := fm.teal
outdir := ./pkg/teal
import := fm.teal:github.com/teal-fm/piper/lexicons/api
# relative to this makefile
lexicon_dir := ./api
atproto_lex_dir := ../atproto/lexicons
