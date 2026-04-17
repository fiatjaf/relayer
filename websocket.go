package relayer

import (
	"sync"

	"github.com/fasthttp/websocket"
	"github.com/nbd-wtf/go-nostr/nip77/negentropy"
	"golang.org/x/time/rate"
)

type WebSocket struct {
	conn  *websocket.Conn
	mutex sync.Mutex

	// nip42
	challenge string
	authed    string
	limiter   *rate.Limiter

	// nip77
	negsMu sync.Mutex
	negs   map[string]*negentropy.Negentropy
}

func (ws *WebSocket) WriteJSON(any interface{}) error {
	ws.mutex.Lock()
	defer ws.mutex.Unlock()
	return ws.conn.WriteJSON(any)
}

func (ws *WebSocket) WriteMessage(t int, b []byte) error {
	ws.mutex.Lock()
	defer ws.mutex.Unlock()
	return ws.conn.WriteMessage(t, b)
}
