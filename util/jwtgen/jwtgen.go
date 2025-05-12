// bless up @haileyok
// https://github.com/haileyok/atproto-oauth-golang/blob/main/helpers/generic.go

package jwtgen

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"time"

	"github.com/lestrrat-go/jwx/v2/jwk"
)

func WriteJwksToDisk(inputPrefix string) error {
	var prefix *string
	if inputPrefix != "" {
		prefix = &inputPrefix
	}
	key, err := GenerateKey(prefix)
	if err != nil {
		return fmt.Errorf("error generating key: %v\n", err)
	}

	b, err := json.Marshal(key)
	if err != nil {
		return fmt.Errorf("error marshaling key: %v\n", err)
	}

	if err := os.WriteFile("./jwks.json", b, 0644); err != nil {
		return fmt.Errorf("error writing jwk to disk: %v\n", err)
	}

	return nil
}

func GenerateKey(kidPrefix *string) (jwk.Key, error) {
	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}

	key, err := jwk.FromRaw(privKey)
	if err != nil {
		return nil, err
	}

	var kid string
	if kidPrefix != nil {
		kid = fmt.Sprintf("%s-%d", *kidPrefix, time.Now().Unix())
	} else {
		kid = fmt.Sprintf("%d", time.Now().Unix())
	}

	if err := key.Set(jwk.KeyIDKey, kid); err != nil {
		return nil, err
	}
	return key, nil
}

func IsUrlSafeAndParsed(rawString string) (*url.URL, error) {
	u, err := url.Parse(rawString)
	if err != nil {
		return nil, err
	}

	if u.Scheme != "https" {
		return nil, fmt.Errorf("input url is not https")
	}

	if u.Hostname() == "" {
		return nil, fmt.Errorf("url hostname was empty")
	}

	if u.User != nil {
		return nil, fmt.Errorf("url user was not empty")
	}

	if u.Port() != "" {
		return nil, fmt.Errorf("url port was not empty")
	}

	return u, nil
}

func GetPrivateKey(key jwk.Key) (*ecdsa.PrivateKey, error) {
	var pkey ecdsa.PrivateKey
	if err := key.Raw(&pkey); err != nil {
		return nil, err
	}

	return &pkey, nil
}

func GetPublicKey(key jwk.Key) (*ecdsa.PublicKey, error) {
	var pkey ecdsa.PublicKey
	if err := key.Raw(&pkey); err != nil {
		return nil, err
	}

	return &pkey, nil
}

type JwksResponseObject struct {
	Keys []jwk.Key `json:"keys"`
}

func CreateJwksResponseObject(key jwk.Key) *JwksResponseObject {
	return &JwksResponseObject{
		Keys: []jwk.Key{key},
	}
}

func ParseJWKFromBytes(bytes []byte) (jwk.Key, error) {
	return jwk.ParseKey(bytes)
}
