package atproto

import (
	"context"
	"fmt"
	"net/url"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/client"
	"github.com/teal-fm/piper/api/teal"
)

// tealProfileCollection is where a teal.fm profile lives in a user's repo.
const tealProfileCollection = "fm.teal.actor.profile"

// TealProfile is what piper shows from a teal.fm profile record. Either field
// may be empty, including for accounts that have no such record at all.
type TealProfile struct {
	DisplayName string
	AvatarURL   string
}

// TealProfile reads the account's teal.fm profile.
func (a *AuthService) TealProfile(ctx context.Context, did, sessionID string) (TealProfile, error) {
	apiClient, err := a.GetATProtoClient(did, sessionID, ctx)
	if err != nil || apiClient == nil {
		return TealProfile{}, fmt.Errorf("failed to get ATProto client: %w", err)
	}
	return tealProfile(ctx, apiClient, did), nil
}

func tealProfile(ctx context.Context, apiClient *client.APIClient, did string) TealProfile {
	record, err := comatproto.RepoGetRecord(ctx, apiClient, "", tealProfileCollection, did, "self")
	if err != nil {
		// Some users (like me) don't have a teal.fm profile,
		// so this isn't a "real" error.
		return TealProfile{}
	}

	profile, ok := record.Value.Val.(*teal.ActorProfile)
	if !ok {
		return TealProfile{}
	}

	var out TealProfile
	if profile.DisplayName != nil {
		out.DisplayName = *profile.DisplayName
	}
	if profile.Avatar != nil {
		out.AvatarURL = fmt.Sprintf("%s/xrpc/com.atproto.sync.getBlob?did=%s&cid=%s",
			apiClient.Host, url.QueryEscape(did), url.QueryEscape(profile.Avatar.Ref.String()))
	}
	return out
}
