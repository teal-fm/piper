package atproto

// Stolen from https://github.com/haileyok/atproto-oauth-golang/blob/f780d3716e2b8a06c87271a2930894319526550e/cmd/web_server_demo/resolution.go

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"

	"github.com/bluesky-social/indigo/atproto/syntax"
	oauth "github.com/haileyok/atproto-oauth-golang"
)

// user information struct
type UserInformation struct {
	AuthService string                            `json:"authService"`
	AuthServer  string                            `json:"authServer"`
	AuthMeta    *oauth.OauthAuthorizationMetadata `json:"authMeta"`
	// do NOT save the current handle permanently!
	Handle string `json:"handle"`
	DID    string `json:"did"`
}

type Identity struct {
	AlsoKnownAs []string `json:"alsoKnownAs"`
	Service     []struct {
		ID              string `json:"id"`
		Type            string `json:"type"`
		ServiceEndpoint string `json:"serviceEndpoint"`
	} `json:"service"`
}

func (a *ATprotoAuthService) getUserInformation(ctx context.Context, handleOrDid string) (*UserInformation, error) {
	cli := a.client

	// if we have a did skip this
	did := handleOrDid
	err := error(nil)
	// technically checking SHOULD be more rigorous.
	if !strings.HasPrefix(handleOrDid, "did:") {
		did, err = resolveHandle(ctx, did)
		if err != nil {
			return nil, err
		}
	} else {
		did = handleOrDid
	}

	doc, err := getIdentityDocument(ctx, did)
	if err != nil {
		return nil, err
	}

	service, err := getAtprotoPdsService(doc)
	if err != nil {
		return nil, err
	}

	authserver, err := cli.ResolvePdsAuthServer(ctx, service)
	if err != nil {
		return nil, err
	}

	authmeta, err := cli.FetchAuthServerMetadata(ctx, authserver)
	if err != nil {
		return nil, err
	}

	if len(doc.AlsoKnownAs) == 0 {
		return nil, fmt.Errorf("alsoKnownAs is empty, couldn't acquire handle: %w", err)

	}
	handle := strings.Replace(doc.AlsoKnownAs[0], "at://", "", 1)

	return &UserInformation{
		AuthService: service,
		AuthServer:  authserver,
		AuthMeta:    authmeta,
		Handle:      handle,
		DID:         did,
	}, nil
}

func resolveHandle(ctx context.Context, handle string) (string, error) {
	var did string

	_, err := syntax.ParseHandle(handle)
	if err != nil {
		return "", err
	}

	recs, err := net.LookupTXT(fmt.Sprintf("_atproto.%s", handle))
	if err == nil {
		for _, rec := range recs {
			if strings.HasPrefix(rec, "did=") {
				did = strings.Split(rec, "did=")[1]
				break
			}
		}
	}

	if did == "" {
		req, err := http.NewRequestWithContext(
			ctx,
			"GET",
			fmt.Sprintf("https://%s/.well-known/atproto-did", handle),
			nil,
		)
		if err != nil {
			return "", err
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			io.Copy(io.Discard, resp.Body)
			return "", fmt.Errorf("unable to resolve handle")
		}

		b, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", err
		}

		maybeDid := string(b)

		if _, err := syntax.ParseDID(maybeDid); err != nil {
			return "", fmt.Errorf("unable to resolve handle")
		}

		did = maybeDid
	}

	return did, nil
}

// Get the Identity document for a given DID
func getIdentityDocument(ctx context.Context, did string) (*Identity, error) {
	var ustr string
	if strings.HasPrefix(did, "did:plc:") {
		ustr = fmt.Sprintf("https://plc.directory/%s", did)
	} else if strings.HasPrefix(did, "did:web:") {
		ustr = fmt.Sprintf("https://%s/.well-known/did.json", strings.TrimPrefix(did, "did:web:"))
	} else {
		return nil, fmt.Errorf("did was not a supported did type")
	}

	req, err := http.NewRequestWithContext(ctx, "GET", ustr, nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, resp.Body)
		return nil, fmt.Errorf("could not find identity in plc registry")
	}

	var identity Identity
	if err := json.NewDecoder(resp.Body).Decode(&identity); err != nil {
		return nil, err
	}

	return &identity, nil
}

// Get the atproto PDS service endpoint from an Identity document
func getAtprotoPdsService(identity *Identity) (string, error) {
	var service string
	for _, svc := range identity.Service {
		if svc.ID == "#atproto_pds" {
			service = svc.ServiceEndpoint
			break
		}
	}

	if service == "" {
		return "", fmt.Errorf("could not find atproto_pds service in identity services")
	}

	return service, nil
}

func resolveServiceFromDoc(identity *Identity) (string, error) {
	service, err := getAtprotoPdsService(identity)
	if err != nil {
		return "", err
	}

	return service, nil
}
