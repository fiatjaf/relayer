package relayer

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/fasthttp/websocket"
	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip11"
	"github.com/nbd-wtf/go-nostr/nip42"
	"golang.org/x/exp/slices"
	"golang.org/x/time/rate"
)

// TODO: consider moving these to Server as config params
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

// TODO: consider moving these to Server as config params
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

func (s *Server) HandleWebsocket(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	store := s.relay.Storage(ctx)
	advancedDeleter, _ := store.(AdvancedDeleter)

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.Log.Errorf("failed to upgrade websocket: %v", err)
		return
	}
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()
	s.clients[conn] = struct{}{}
	ticker := time.NewTicker(pingPeriod)
	stop := make(chan struct{})

	s.Log.Infof("connected from %s", conn.RemoteAddr().String())

	// NIP-42 challenge
	challenge := make([]byte, 8)
	rand.Read(challenge)

	ws := &WebSocket{
		conn:      conn,
		challenge: hex.EncodeToString(challenge),
	}

	if s.options.perConnectionLimiter != nil {
		ws.limiter = rate.NewLimiter(
			s.options.perConnectionLimiter.Limit(),
			s.options.perConnectionLimiter.Burst(),
		)
	}

	// reader
	go func() {
		defer func() {
			ticker.Stop()
			stop <- struct{}{}
			close(stop)
			s.clientsMu.Lock()
			if _, ok := s.clients[conn]; ok {
				conn.Close()
				delete(s.clients, conn)
				removeListener(ws)
			}
			s.clientsMu.Unlock()
			s.Log.Infof("disconnected from %s", conn.RemoteAddr().String())
		}()

		conn.SetReadLimit(maxMessageSize)
		conn.SetReadDeadline(time.Now().Add(pongWait))
		conn.SetPongHandler(func(string) error {
			conn.SetReadDeadline(time.Now().Add(pongWait))
			return nil
		})

		// NIP-42 auth challenge
		if _, ok := s.relay.(Auther); ok {
			ws.WriteJSON(nostr.AuthEnvelope{Challenge: &ws.challenge})
		}

		for {
			typ, message, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(
					err,
					websocket.CloseGoingAway,        // 1001
					websocket.CloseNoStatusReceived, // 1005
					websocket.CloseAbnormalClosure,  // 1006
				) {
					s.Log.Warningf("unexpected close error from %s: %v", r.Header.Get("X-Forwarded-For"), err)
				}
				break
			}

			if ws.limiter != nil {
				// NOTE: Wait will throttle the requests.
				// To reject requests exceeding the limit, use if !ws.limiter.Allow()
				if err := ws.limiter.Wait(context.TODO()); err != nil {
					s.Log.Warningf("unexpected limiter error %v", err)
					continue
				}
			}

			if typ == websocket.PingMessage {
				ws.WriteMessage(websocket.PongMessage, nil)
				continue
			}

			go func(message []byte) {
				ctx = context.TODO()

				var notice string
				defer func() {
					if notice != "" {
						ws.WriteJSON(nostr.NoticeEnvelope(notice))
					}
				}()

				var request []json.RawMessage
				if err := json.Unmarshal(message, &request); err != nil {
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
					latestInex := len(request) - 1
					// it's a new event
					var evt nostr.Event
					if err := json.Unmarshal(request[latestInex], &evt); err != nil {
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
						reason := "error: failed to verify signature"
						ws.WriteJSON(nostr.OKEnvelope{EventID: evt.ID, OK: false, Reason: &reason})
						return
					} else if !ok {
						reason := "invalid: signature is invalid"
						ws.WriteJSON(nostr.OKEnvelope{EventID: evt.ID, OK: false, Reason: &reason})
						return
					}

					if evt.Kind == 5 {
						// event deletion -- nip09
						for _, tag := range evt.Tags {
							if len(tag) >= 2 && tag[0] == "e" {
								if advancedDeleter != nil {
									advancedDeleter.BeforeDelete(ctx, tag[1], evt.PubKey)
								}

								if err := store.DeleteEvent(ctx, tag[1], evt.PubKey); err != nil {
									reason := fmt.Sprintf("error: %s", err.Error())
									ws.WriteJSON(nostr.OKEnvelope{EventID: evt.ID, OK: false, Reason: &reason})
									return
								}

								if advancedDeleter != nil {
									advancedDeleter.AfterDelete(tag[1], evt.PubKey)
								}
							}
						}
						return
					}

					ok, message := AddEvent(ctx, s.relay, &evt)
					var reason *string
					if message != "" {
						reason = &message
					}
					ws.WriteJSON(nostr.OKEnvelope{EventID: evt.ID, OK: ok, Reason: reason})
				case "COUNT":
					counter, ok := store.(EventCounter)
					if !ok {
						notice = "restricted: this relay does not support NIP-45"
						return
					}

					var id string
					json.Unmarshal(request[1], &id)
					if id == "" {
						notice = "COUNT has no <id>"
						return
					}

					total := int64(0)
					filters := make(nostr.Filters, len(request)-2)
					for i, filterReq := range request[2:] {
						if err := json.Unmarshal(filterReq, &filters[i]); err != nil {
							notice = "failed to decode filter"
							return
						}

						filter := &filters[i]

						// prevent kind-4 events from being returned to unauthed users,
						//   only when authentication is a thing
						if _, ok := s.relay.(Auther); ok {
							if slices.Contains(filter.Kinds, 4) {
								senders := filter.Authors
								receivers := filter.Tags["p"]
								switch {
								case ws.authed == "":
									// not authenticated
									notice = "restricted: this relay does not serve kind-4 to unauthenticated users, does your client implement NIP-42?"
									return
								case len(senders) == 1 && len(receivers) < 2 && (senders[0] == ws.authed):
									// allowed filter: ws.authed is sole sender (filter specifies one or all receivers)
								case len(receivers) == 1 && len(senders) < 2 && (receivers[0] == ws.authed):
									// allowed filter: ws.authed is sole receiver (filter specifies one or all senders)
								default:
									// restricted filter: do not return any events,
									//   even if other elements in filters array were not restricted).
									//   client should know better.
									notice = "restricted: authenticated user does not have authorization for requested filters."
									return
								}
							}
						}

						count, err := counter.CountEvents(ctx, filter)
						if err != nil {
							s.Log.Errorf("store: %v", err)
							continue
						}
						total += count
					}

					ws.WriteJSON([]interface{}{"COUNT", id, map[string]int64{"count": total}})
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

						// prevent kind-4 events from being returned to unauthed users,
						//   only when authentication is a thing
						if _, ok := s.relay.(Auther); ok {
							if slices.Contains(filter.Kinds, 4) {
								senders := filter.Authors
								receivers := filter.Tags["p"]
								switch {
								case ws.authed == "":
									// not authenticated
									notice = "restricted: this relay does not serve kind-4 to unauthenticated users, does your client implement NIP-42?"
									return
								case len(senders) == 1 && len(receivers) < 2 && (senders[0] == ws.authed):
									// allowed filter: ws.authed is sole sender (filter specifies one or all receivers)
								case len(receivers) == 1 && len(senders) < 2 && (receivers[0] == ws.authed):
									// allowed filter: ws.authed is sole receiver (filter specifies one or all senders)
								default:
									// restricted filter: do not return any events,
									//   even if other elements in filters array were not restricted).
									//   client should know better.
									notice = "restricted: authenticated user does not have authorization for requested filters."
									return
								}
							}
						}

						events, err := store.QueryEvents(ctx, filter)
						if err != nil {
							s.Log.Errorf("store: %v", err)
							continue
						}

						// ensures the client won't be bombarded with events in case Storage doesn't do limits right
						if filter.Limit == 0 {
							filter.Limit = 9999999999
						}
						i := 0
						for event := range events {
							ws.WriteJSON(nostr.EventEnvelope{SubscriptionID: &id, Event: *event})
							i++
							if i > filter.Limit {
								break
							}
						}

						// exhaust the channel (in case we broke out of it early) so it is closed by the storage
						for range events {
						}
					}

					ws.WriteJSON(nostr.EOSEEnvelope(id))
					setListener(id, ws, filters)
				case "CLOSE":
					var id string
					json.Unmarshal(request[1], &id)
					if id == "" {
						notice = "CLOSE has no <id>"
						return
					}

					removeListenerId(ws, id)
				case "AUTH":
					if auther, ok := s.relay.(Auther); ok {
						var evt nostr.Event
						if err := json.Unmarshal(request[1], &evt); err != nil {
							notice = "failed to decode auth event: " + err.Error()
							return
						}
						if pubkey, ok := nip42.ValidateAuthEvent(&evt, ws.challenge, auther.ServiceURL()); ok {
							ws.authed = pubkey
							ctx = context.WithValue(ctx, AUTH_CONTEXT_KEY, pubkey)
							ws.WriteJSON(nostr.OKEnvelope{EventID: evt.ID, OK: true})
						} else {
							reason := "error: failed to authenticate"
							ws.WriteJSON(nostr.OKEnvelope{EventID: evt.ID, OK: false, Reason: &reason})
						}
					}
				default:
					if cwh, ok := s.relay.(CustomWebSocketHandler); ok {
						cwh.HandleUnknownType(ws, typ, request)
					} else {
						notice = "unknown message type " + typ
					}
				}
			}(message)
		}
	}()

	// writer
	go func() {
		defer func() {
			ticker.Stop()
			conn.Close()
			for := range stop {
			}
		}()

		for {
			select {
			case <-ticker.C:
				conn.SetWriteDeadline(time.Now().Add(writeWait))
				err := ws.WriteMessage(websocket.PingMessage, nil)
				if err != nil {
					s.Log.Errorf("error writing ping: %v; closing websocket", err)
					return
				}
				s.Log.Infof("pinging for %s", conn.RemoteAddr().String())
			case <-stop:
				return
			}
		}
	}()
}

func (s *Server) HandleNIP11(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	supportedNIPs := []int{9, 11, 12, 15, 16, 20, 33}
	if _, ok := s.relay.(Auther); ok {
		supportedNIPs = append(supportedNIPs, 42)
	}
	if storage, ok := s.relay.(Storage); ok && storage != nil {
		if _, ok = storage.(EventCounter); ok {
			supportedNIPs = append(supportedNIPs, 45)
		}
	}

	info := nip11.RelayInformationDocument{
		Name:          s.relay.Name(),
		Description:   "relay powered by the relayer framework",
		PubKey:        "~",
		Contact:       "~",
		SupportedNIPs: supportedNIPs,
		Software:      "https://github.com/fiatjaf/relayer",
		Version:       "~",
	}

	if ifmer, ok := s.relay.(Informationer); ok {
		info = ifmer.GetNIP11InformationDocument()
	}

	json.NewEncoder(w).Encode(info)
}
