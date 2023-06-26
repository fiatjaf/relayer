package relayer

import "context"

const AUTH_CONTEXT_KEY = iota

func GetAuthStatus(ctx context.Context) (pubkey string, ok bool) {
	authedPubkey := ctx.Value(AUTH_CONTEXT_KEY)
	if authedPubkey == nil {
		return "", false
	}
	return authedPubkey.(string), true
}
