package main

import (
	"reflect"

	"github.com/bluesky-social/indigo/mst"
	"github.com/teal-fm/piper/api/teal"

	cbg "github.com/whyrusleeping/cbor-gen"
)

func main() {
	var typVals []any
	for _, typ := range mst.CBORTypes() {
		typVals = append(typVals,
			reflect.New(typ).Elem().Interface())
	}

	genCfg := cbg.Gen{
		MaxStringLength: 1_000_000,
	}

	if err :=
		genCfg.WriteMapEncodersToFile("api/teal/cbor_gen.go",
			"teal",
			teal.AlphaFeedPlay{},
			teal.AlphaActorProfile{},
			teal.AlphaActorStatus{},
			teal.AlphaActorProfile_FeaturedItem{},
			teal.AlphaFeedDefs_PlayView{},
			teal.AlphaFeedDefs_Artist{},
		); err != nil {
		panic(err)
	}
}
