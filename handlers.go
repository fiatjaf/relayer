package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
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

func handleWebsocket(w http.ResponseWriter, r *http.Request) {
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
					err = errors.New("request has less than parameters")
					return
				}
				if err != nil {
					return
				}

				var typ string
				json.Unmarshal(request[0], &typ)

				switch typ {
				case "EVENT":
					// it's a new event
					err = saveEvent(request[1])

				case "REQ":
					var id string
					json.Unmarshal(request[0], &id)
					if id == "" {
						err = errors.New("REQ has no <id>")
						return
					}

					filters := make([]*filter.EventFilter, len(request)-2)
					for i, filterReq := range request[2:] {
						err = json.Unmarshal(filterReq, &filters[i])
						if err != nil {
							return
						}

						events, err := queryEvents(filters[i])
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

					removeListener(id)
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
				err := conn.WriteMessage(websocket.TextMessage, []byte("PING"))
				if err != nil {
					log.Warn().Err(err).Msg("error writing ping, closing websocket")
					return
				}
				conn.WriteMessage(websocket.PingMessage, nil)
			}
		}
	}()
}

func saveEvent(body []byte) error {
	var evt event.Event
	err := json.Unmarshal(body, &evt)
	if err != nil {
		log.Warn().Err(err).Str("body", string(body)).Msg("couldn't decode body")
		return errors.New("failed to decode event")
	}

	// disallow large contents
	if len(evt.Content) > 1000 {
		log.Warn().Err(err).Msg("event content too large")
		return errors.New("event content too large")
	}

	// check serialization
	serialized := evt.Serialize()

	// assign ID
	hash := sha256.Sum256(serialized)
	evt.ID = hex.EncodeToString(hash[:])

	// check signature (requires the ID to be set)
	if ok, err := evt.CheckSignature(); err != nil {
		log.Warn().Err(err).Msg("signature verification error")
		return errors.New("signature verification error")
	} else if !ok {
		log.Warn().Err(err).Msg("signature invalid")
		return errors.New("signature invalid")
	}

	// react to different kinds of events
	switch evt.Kind {
	case event.KindSetMetadata:
		// delete past set_metadata events from this user
		db.Exec(`DELETE FROM event WHERE pubkey = $1 AND kind = 0`, evt.PubKey)
	case event.KindTextNote:
		// do nothing
	case event.KindRecommendServer:
		// delete past recommend_server events equal to this one
		db.Exec(`DELETE FROM event WHERE pubkey = $1 AND kind = 2 AND content = $2`,
			evt.PubKey, evt.Content)
	case event.KindContactList:
		// delete past contact lists from this same pubkey
		db.Exec(`DELETE FROM event WHERE pubkey = $1 AND kind = 3`, evt.PubKey)
	}

	// insert
	tagsj, _ := json.Marshal(evt.Tags)
	_, err = db.Exec(`
        INSERT INTO event (id, pubkey, created_at, kind, tags, content, sig)
        VALUES ($1, $2, $3, $4, $5, $6, $7)
    `, evt.ID, evt.PubKey, evt.CreatedAt, evt.Kind, tagsj, evt.Content, evt.Sig)
	if err != nil {
		if strings.Index(err.Error(), "UNIQUE") != -1 {
			// already exists
			return nil
		}

		log.Warn().Err(err).Str("pubkey", evt.PubKey).Msg("failed to save")
		return errors.New("failed to save event")
	}

	notifyListeners(&evt)
	return nil
}
