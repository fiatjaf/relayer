package relayer

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"

	"github.com/fiatjaf/go-nostr"
	"github.com/gorilla/websocket"
)

const (
	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer.
	pongWait = 60 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = pongWait / 2

	// Maximum message size allowed from peer.
	maxMessageSize = 512000
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

func handleWebsocket(relay Relay) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Warn().Err(err).Msg("failed to upgrade websocket")
			return
		}
		ticker := time.NewTicker(pingPeriod)

		ws := &WebSocket{conn: conn}

		// reader
		go func() {
			defer func() {
				ticker.Stop()
				conn.Close()
			}()

			conn.SetReadLimit(maxMessageSize)
			conn.SetReadDeadline(time.Now().Add(pongWait))
			conn.SetPongHandler(func(string) error {
				conn.SetReadDeadline(time.Now().Add(pongWait))
				return nil
			})

			for {
				typ, message, err := conn.ReadMessage()
				if err != nil {
					if websocket.IsUnexpectedCloseError(
						err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
						log.Warn().Err(err).Msg("unexpected close error")
					}
					break
				}

				if typ == websocket.PingMessage {
					ws.WriteMessage(websocket.PongMessage, nil)
					continue
				}

				go func(message []byte) {
					var notice string
					defer func() {
						if notice != "" {
							ws.WriteJSON([]interface{}{"NOTICE", notice})
						}
					}()

					var request []json.RawMessage
					if err := json.Unmarshal(message, &request); err != nil {
						// stop silently
						return
					}

					if len(request) < 2 {
						notice = "request has less than 2 parameters"
						return
					}

					var typ string
					json.Unmarshal(request[0], &typ)

					switch typ {
					case "EVENT":
						// it's a new event
						var evt nostr.Event
						if err := json.Unmarshal(request[1], &evt); err != nil {
							notice = "failed to decode event: " + err.Error()
							return
						}

						// check serialization
						serialized := evt.Serialize()

						// assign ID
						hash := sha256.Sum256(serialized)
						evt.ID = hex.EncodeToString(hash[:])

						// check signature (requires the ID to be set)
						if ok, err := evt.CheckSignature(); err != nil {
							notice = "signature verification error"
							return
						} else if !ok {
							notice = "signature invalid"
							return
						}

						err = relay.SaveEvent(&evt)
						if err != nil {
							notice = err.Error()
							return
						}

						notifyListeners(&evt)
						break
					case "REQ":
						var id string
						json.Unmarshal(request[1], &id)
						if id == "" {
							notice = "REQ has no <id>"
							return
						}

						filters := make(nostr.EventFilters, len(request)-2)
						for i, filterReq := range request[2:] {
							if err := json.Unmarshal(
								filterReq,
								&filters[i],
							); err != nil {
								notice = "failed to decode filter"
								return
							}

							events, err := relay.QueryEvents(&filters[i])
							if err == nil {
								for _, event := range events {
									ws.WriteJSON([]interface{}{"EVENT", id, event})
								}
							}
						}

						setListener(id, ws, filters)
						break
					case "CLOSE":
						var id string
						json.Unmarshal(request[0], &id)
						if id == "" {
							notice = "CLOSE has no <id>"
							return
						}

						removeListener(ws, id)
						break
					default:
						notice = "unknown message type " + typ
						return
					}
				}(message)
			}
		}()

		// writer
		go func() {
			defer func() {
				ticker.Stop()
				conn.Close()
			}()

			for {
				select {
				case <-ticker.C:
					err := ws.WriteMessage(websocket.PingMessage, nil)
					if err != nil {
						log.Warn().Err(err).Msg("error writing ping, closing websocket")
						return
					}
				}
			}
		}()
	}
}
