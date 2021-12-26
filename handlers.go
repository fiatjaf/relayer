package relayer

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/fiatjaf/go-nostr/event"
	"github.com/fiatjaf/go-nostr/filter"
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

		// reader
		go func() {
			defer func() {
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
					conn.WriteMessage(websocket.PongMessage, nil)
					continue
				}

				go func(message []byte) {
					var err error

					defer func() {
						if err != nil {
							conn.WriteJSON([]interface{}{"NOTICE", err.Error()})
						}
					}()

					var request []json.RawMessage
					err = json.Unmarshal(message, &request)
					if err == nil && len(request) < 2 {
						err = errors.New("request has less than 2 parameters")
						return
					}
					if err != nil {
						err = nil
						return
					}

					var typ string
					json.Unmarshal(request[0], &typ)

					switch typ {
					case "EVENT":
						// it's a new event
						var evt event.Event
						err := json.Unmarshal(request[1], &evt)
						if err != nil {
							err = fmt.Errorf("failed to decode event: %w", err)
							return
						}

						// check serialization
						serialized := evt.Serialize()

						// assign ID
						hash := sha256.Sum256(serialized)
						evt.ID = hex.EncodeToString(hash[:])

						// check signature (requires the ID to be set)
						if ok, err := evt.CheckSignature(); err != nil {
							err = errors.New("signature verification error")
							return
						} else if !ok {
							err = errors.New("signature invalid")
							return
						}

						err = relay.SaveEvent(&evt)
						if err != nil {
							return
						}

						notifyListeners(&evt)
					case "REQ":
						var id string
						json.Unmarshal(request[1], &id)
						if id == "" {
							err = errors.New("REQ has no <id>")
							return
						}

						filters := make(filter.EventFilters, len(request)-2)
						for i, filterReq := range request[2:] {
							err = json.Unmarshal(filterReq, &filters[i])
							if err != nil {
								return
							}

							events, err := relay.QueryEvents(&filters[i])
							if err == nil {
								for _, event := range events {
									conn.WriteJSON([]interface{}{"EVENT", id, event})
								}
							}
						}

						setListener(id, conn, filters)

					case "CLOSE":
						var id string
						json.Unmarshal(request[0], &id)
						if id == "" {
							err = errors.New("CLOSE has no <id>")
							return
						}

						removeListener(conn, id)
					}
				}(message)
			}
		}()

		// writer
		go func() {
			ticker := time.NewTicker(pingPeriod)
			defer func() {
				ticker.Stop()
				conn.Close()
			}()

			for {
				select {
				case <-ticker.C:
					err := conn.WriteMessage(websocket.PingMessage, nil)
					if err != nil {
						log.Warn().Err(err).Msg("error writing ping, closing websocket")
						return
					}
				}
			}
		}()
	}
}
