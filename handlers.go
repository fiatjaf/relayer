package relayer

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip11"
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
		store := relay.Storage()
		advancedDeleter, _ := store.(AdvancedDeleter)
		advancedQuerier, _ := store.(AdvancedQuerier)

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
						err,
						websocket.CloseGoingAway,        // 1001
						websocket.CloseNoStatusReceived, // 1005
						websocket.CloseAbnormalClosure,  // 1006
					) {
						log.Warn().Err(err).Str("ip", r.Header.Get("x-forwarded-for")).Msg("unexpected close error")
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
							ws.WriteJSON([]interface{}{"OK", evt.ID, false, "error: failed to verify signature"})
							return
						} else if !ok {
							ws.WriteJSON([]interface{}{"OK", evt.ID, false, "invalid: signature is invalid"})
							return
						}

						if evt.Kind == 5 {
							// event deletion -- nip09
							for _, tag := range evt.Tags {
								if len(tag) >= 2 && tag[0] == "e" {
									if advancedDeleter != nil {
										advancedDeleter.BeforeDelete(tag[1], evt.PubKey)
									}

									if err := store.DeleteEvent(tag[1], evt.PubKey); err != nil {
										ws.WriteJSON([]interface{}{"OK", evt.ID, false, fmt.Sprintf("error: %s", err.Error())})
										return
									}

									if advancedDeleter != nil {
										advancedDeleter.AfterDelete(tag[1], evt.PubKey)
									}
								}
							}
							return
						}

						ok, message := AddEvent(relay, evt)
						ws.WriteJSON([]interface{}{"OK", evt.ID, ok, message})

						break
					case "REQ":
						var id string
						json.Unmarshal(request[1], &id)
						if id == "" {
							notice = "REQ has no <id>"
							return
						}

						filters := make(nostr.Filters, len(request)-2)
						for i, filterReq := range request[2:] {
							if err := json.Unmarshal(
								filterReq,
								&filters[i],
							); err != nil {
								notice = "failed to decode filter"
								return
							}

							filter := &filters[i]

							if advancedQuerier != nil {
								advancedQuerier.BeforeQuery(filter)
							}

							events, err := store.QueryEvents(filter)
							if err == nil {
								if advancedQuerier != nil {
									advancedQuerier.AfterQuery(events, filter)
								}

								if filter.Limit > 0 && len(events) > filter.Limit {
									events = events[0:filter.Limit]
								}

								for _, event := range events {
									ws.WriteJSON([]interface{}{"EVENT", id, event})
								}

								ws.WriteJSON([]interface{}{"EOSE", id})
							}
						}

						setListener(id, ws, filters)
						break
					case "CLOSE":
						var id string
						json.Unmarshal(request[1], &id)
						if id == "" {
							notice = "CLOSE has no <id>"
							return
						}

						removeListener(ws, id)
						break
					default:
						if cwh, ok := relay.(CustomWebSocketHandler); ok {
							cwh.HandleUnknownType(ws, typ, request)
						} else {
							notice = "unknown message type " + typ
						}
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

func handleNIP11(relay Relay) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		info := nip11.RelayInformationDocument{
			Name:          relay.Name(),
			Description:   "relay powered by the relayer framework",
			PubKey:        "~",
			Contact:       "~",
			SupportedNIPs: []int{9, 15, 16},
			Software:      "https://github.com/fiatjaf/relayer",
			Version:       "~",
		}

		if ifmer, ok := relay.(Informationer); ok {
			info = ifmer.GetNIP11InformationDocument()
		}

		json.NewEncoder(w).Encode(info)
	}
}
