package relayer

import "context"

const AUTH_CONTEXT_KEY = iota

func GetAuthStatus(ctx context.Context) (pubkey string, ok bool) {
	value := ctx.Value(AUTH_CONTEXT_KEY)
	if value == nil {
		return "", false
	}
	if ws, ok := value.(*WebSocket); ok {
		return ws.authed, true
	}
	return "", false
}
