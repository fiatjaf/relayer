package relayer

import "context"

const (
	AUTH_CONTEXT_KEY = iota
	SERVER_CONTEXT_KEY
)

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

func getServer(ctx context.Context) (*Server, bool) {
	value := ctx.Value(SERVER_CONTEXT_KEY)
	if value == nil {
		return nil, false
	}
	srv, ok := value.(*Server)
	return srv, ok
}
