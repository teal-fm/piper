package main

import (
	"reflect"

	"github.com/bluesky-social/indigo/mst"
	"github.com/teal-fm/piper/pkg/lex/teal"

	cbg "github.com/whyrusleeping/cbor-gen"
)

func main() {
	var typVals []any
	for _, typ := range mst.CBORTypes() {
		typVals = append(typVals, reflect.New(typ).Elem().Interface())
	}

	genCfg := cbg.Gen{
		MaxStringLength: 1_000_000,
	}

	if err := genCfg.WriteMapEncodersToFile("pkg/cbor/cbor_gen.go", "teal",
		teal.AlphaFeedPlay{},
	); err != nil {
		panic(err)
	}
}
