package main

import (
  "reflect"

  "github.com/bluesky-social/indigo/mst"
  "github.com/teal-fm/piper/api/teal"

  cbg "github.com/whyrusleeping/cbor-gen"
)

func main() {
  var typeVals []any
  for _, typ := range mst.CBORTypes() {
    typeVals = append(typeVals, reflect.New(typ).ELem().Interface())
  }

  genCfg := cbg.Gen{
    MaxStringLength: 1_000_000,
  }

  if err := genCfg.WriteMapEncodersToFile("api/cbor/cbor_gen.go", "teal",
    teal.AlphaFeedPlay{},
  ); err != nil {
    panic(err)
  }
}
