package relayer

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/fasthttp/websocket"
	"github.com/fiatjaf/eventstore"
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

func challenge(conn *websocket.Conn) *WebSocket {
	// NIP-42 challenge
	challenge := make([]byte, 8)
	rand.Read(challenge)

	return &WebSocket{
		conn:      conn,
		challenge: hex.EncodeToString(challenge),
	}
}

func (s *Server) doEvent(ctx context.Context, ws *WebSocket, request []json.RawMessage, store eventstore.Store) string {
	advancedDeleter, _ := store.(AdvancedDeleter)
	latestIndex := len(request) - 1

	// it's a new event
	var evt nostr.Event
	if err := json.Unmarshal(request[latestIndex], &evt); err != nil {
		return "failed to decode event: " + err.Error()
	}

	// check id
	hash := sha256.Sum256(evt.Serialize())
	if id := hex.EncodeToString(hash[:]); id != evt.ID {
		reason := "invalid: event id is computed incorrectly"
		ws.WriteJSON(nostr.OKEnvelope{EventID: evt.ID, OK: false, Reason: reason})
		return ""
	}

	// check signature
	if ok, err := evt.CheckSignature(); err != nil {
		ws.WriteJSON(nostr.OKEnvelope{EventID: evt.ID, OK: false, Reason: "error: failed to verify signature"})
		return ""
	} else if !ok {
		ws.WriteJSON(nostr.OKEnvelope{EventID: evt.ID, OK: false, Reason: "invalid: signature is invalid"})
		return ""
	}

	if evt.Kind == 5 {
		// event deletion -- nip09
		for _, tag := range evt.Tags {
			if len(tag) >= 2 && tag[0] == "e" {
				tagCtx, cancel := context.WithTimeout(ctx, time.Millisecond*200)

				// fetch event to be deleted
				res, err := s.relay.Storage(tagCtx).QueryEvents(tagCtx, nostr.Filter{IDs: []string{tag[1]}})
				if err != nil {
					cancel()
					ws.WriteJSON(nostr.OKEnvelope{EventID: evt.ID, OK: false, Reason: "failed to query for target event"})
					return ""
				}

				var target *nostr.Event
				exists := false
				select {
				case target, exists = <-res:
				case <-tagCtx.Done():
				}
				cancel()
				if !exists {
					// this will happen if event is not in the database
					// or when when the query is taking too long, so we just give up
					continue
				}

				// check if this can be deleted
				if target.Kind == nostr.KindGiftWrap {
					if !target.Tags.ContainsAny("p", []string{evt.PubKey}) {
						ws.WriteJSON(nostr.OKEnvelope{EventID: evt.ID, OK: false, Reason: "only the recipient can delete gift-wrapped"})
						return ""
					}
				} else if target.PubKey != evt.PubKey {
					ws.WriteJSON(nostr.OKEnvelope{EventID: evt.ID, OK: false, Reason: "insufficient permissions"})
					return ""
				}

				if advancedDeleter != nil {
					advancedDeleter.BeforeDelete(ctx, tag[1], evt.PubKey)
				}

				if err := store.DeleteEvent(ctx, target); err != nil {
					ws.WriteJSON(nostr.OKEnvelope{EventID: evt.ID, OK: false, Reason: fmt.Sprintf("error: %s", err.Error())})
					return ""
				}

				if advancedDeleter != nil {
					advancedDeleter.AfterDelete(tag[1], evt.PubKey)
				}
			}
		}

		s.notifyListeners(&evt)
		ws.WriteJSON(nostr.OKEnvelope{EventID: evt.ID, OK: true})
		return ""
	}

	ok, reason := AddEvent(ctx, s.relay, &evt)
	ws.WriteJSON(nostr.OKEnvelope{EventID: evt.ID, OK: ok, Reason: reason})
	return ""
}

func (s *Server) doCount(ctx context.Context, ws *WebSocket, request []json.RawMessage, store eventstore.Store) string {
	counter, ok := store.(EventCounter)
	if !ok {
		return "restricted: this relay does not support NIP-45"
	}

	var id string
	json.Unmarshal(request[1], &id)
	if id == "" {
		return "COUNT has no <id>"
	}

	filters := make(nostr.Filters, len(request)-2)
	for i, filterReq := range request[2:] {
		if err := json.Unmarshal(filterReq, &filters[i]); err != nil {
			return "failed to decode filter"
		}

		if reason := s.validateFilterAccess(ws, filters[i], false); reason != "" {
			return reason
		}
	}

	total := int64(0)
	// NIP-45 OR's the filters together into a single count, so an event
	// matching more than one of them counts once. Only a store that can
	// evaluate the union knows which events those are; without one the counts
	// are summed, which is exact for filters that do not overlap.
	if union, ok := store.(FiltersCounter); ok && len(filters) > 1 {
		count, err := union.CountEventsFilters(ctx, filters)
		if err != nil {
			s.Log.Errorf("store: %v", err)
			return "error: failed to count events"
		}
		total = count
	} else {
		for _, filter := range filters {
			count, err := counter.CountEvents(ctx, filter)
			if err != nil {
				s.Log.Errorf("store: %v", err)
				continue
			}
			total += count
		}
	}

	ws.WriteJSON([]interface{}{"COUNT", id, map[string]int64{"count": total}})
	return ""
}

func (s *Server) doReq(ctx context.Context, ws *WebSocket, request []json.RawMessage, store eventstore.Store, req *subscriptionRequest) string {
	var id string
	json.Unmarshal(request[1], &id)
	if id == "" {
		return "REQ has no <id>"
	}
	defer s.finishRequest(ws, id, req)
	if ctx.Err() != nil {
		return ""
	}

	filters := make(nostr.Filters, len(request)-2)
	for i, filterReq := range request[2:] {
		if err := json.Unmarshal(
			filterReq,
			&filters[i],
		); err != nil {
			ws.WriteJSON(nostr.ClosedEnvelope{
				SubscriptionID: id,
				Reason:         "failed to decode filter",
			})
			return ""
		}
	}

	if accepter, ok := s.relay.(ReqAccepter); ok {
		accepted := accepter.AcceptReq(ctx, id, filters, ws.authed)
		if ctx.Err() != nil {
			return ""
		}
		if !accepted {
			ws.WriteJSON(nostr.EOSEEnvelope(id))
			ws.WriteJSON(nostr.ClosedEnvelope{
				SubscriptionID: id,
				Reason:         "REQ filters are not accepted",
			})
			return ""
		}
	}

	// NIP-67: track whether we can give the client a definitive completeness
	// hint on the EOSE. We only claim "finish" when every filter was provably
	// exhausted, and "more" when at least one filter provably had extra events.
	// When a filter stops exactly at its limit it is ambiguous (the storage may
	// have capped the query), so we stay silent -- absence is not definitive.
	anyMore := false
	allFinished := len(filters) > 0

	for _, filter := range filters {
		if ctx.Err() != nil {
			return ""
		}
		if reason := s.validateFilterAccess(ws, filter, true); reason != "" {
			if strings.HasPrefix(reason, "auth-required:") {
				s.sendAuthChallenge(ws)
			}
			ws.WriteJSON(nostr.ClosedEnvelope{
				SubscriptionID: id,
				Reason:         reason,
			})
			return ""
		}
		if filter.LimitZero {
			// the client asked for no stored events, so we never looked; we
			// have no idea whether the relay holds matching ones
			allFinished = false
			continue
		}

		events, err := store.QueryEvents(ctx, filter)
		if err != nil {
			if ctx.Err() != nil {
				return ""
			}
			s.Log.Errorf("store: %v", err)
			allFinished = false
			continue
		}

		// ensures the client won't be bombarded with events in case Storage doesn't do limits right
		if filter.Limit == 0 {
			filter.Limit = 9999999999
		}
		i := 0
		seen := 0
		more := false
		if events != nil {
		readEvents:
			for {
				var event *nostr.Event
				select {
				case <-ctx.Done():
					return ""
				case next, ok := <-events:
					if !ok {
						break readEvents
					}
					event = next
				}
				if ctx.Err() != nil {
					return ""
				}
				seen++
				// Keep draining after the limit, in case storage returns extra
				// events, but let cancellation stop an unfinished stream.
				if i >= filter.Limit {
					// NIP-67: an event we are not going to send proves the
					// relay holds more than it delivered. Events the relay
					// would drop anyway are not "more" from the client's side.
					if s.options.skipEventFunc == nil || !s.options.skipEventFunc(event) {
						more = true
					}
					continue
				}
				if s.options.skipEventFunc != nil && s.options.skipEventFunc(event) {
					continue
				}
				if err := ws.WriteJSON(nostr.EventEnvelope{SubscriptionID: &id, Event: *event}); err != nil {
					return ""
				}
				i++
			}
		}

		// NIP-67 completeness bookkeeping for this filter:
		//   more                 -> definitely more events than we sent
		//   seen == filter.Limit -> ambiguous, the storage may have capped at the limit
		//   otherwise            -> the channel was drained, so we sent everything
		if more {
			anyMore = true
		}
		if more || seen == filter.Limit {
			allFinished = false
		}
	}

	if ctx.Err() != nil {
		return ""
	}
	// TODO: nostr.EOSEEnvelope is a bare string and can't carry the NIP-67
	// hints, so we hand-build the envelope here. Once go-nostr grows a Hints
	// field on EOSEEnvelope, switch this back to writing the typed envelope.
	var eose any = nostr.EOSEEnvelope(id)
	switch {
	case anyMore:
		eose = []any{"EOSE", id, []string{"more"}}
	case allFinished:
		eose = []any{"EOSE", id, []string{"finish"}}
	}
	if err := ws.WriteJSON(eose); err != nil {
		return ""
	}
	s.registerRequest(ws, id, req, filters)
	return ""
}

func (s *Server) doClose(ctx context.Context, ws *WebSocket, request []json.RawMessage, store eventstore.Store) string {
	var id string
	json.Unmarshal(request[1], &id)
	if id == "" {
		return "CLOSE has no <id>"
	}

	s.removeListenerId(ws, id)
	return ""
}

func (s *Server) doAuth(ctx context.Context, ws *WebSocket, request []json.RawMessage, store eventstore.Store) string {
	if auther, ok := s.relay.(Auther); ok {
		var evt nostr.Event
		if err := json.Unmarshal(request[1], &evt); err != nil {
			return "failed to decode auth event: " + err.Error()
		}
		if pubkey, ok := nip42.ValidateAuthEvent(&evt, ws.challenge, auther.ServiceURL()); ok {
			ws.authed = pubkey
			ws.WriteJSON(nostr.OKEnvelope{EventID: evt.ID, OK: true})
		} else {
			ws.WriteJSON(nostr.OKEnvelope{EventID: evt.ID, OK: false, Reason: "error: failed to authenticate"})
		}
	}
	return ""
}

func (s *Server) handleMessage(ctx context.Context, ws *WebSocket, message []byte, store eventstore.Store) {
	var request []json.RawMessage
	if err := json.Unmarshal(message, &request); err != nil {
		// stop silently
		return
	}

	if len(request) < 2 {
		ws.WriteJSON(nostr.NoticeEnvelope("request has less than 2 parameters"))
		return
	}

	var typ string
	json.Unmarshal(request[0], &typ)

	ctx = context.WithValue(ctx, AUTH_CONTEXT_KEY, ws)
	ctx = context.WithValue(ctx, SERVER_CONTEXT_KEY, s)

	// Reserve REQ tokens and process CLOSE in reader order, before starting
	// workers. Ordering only inside doReq would still let a delayed worker
	// start after its CLOSE or after a newer REQ with the same ID.
	var req *subscriptionRequest
	if typ == "REQ" {
		var id string
		json.Unmarshal(request[1], &id)
		if id != "" {
			req = s.beginRequest(ctx, ws, id)
			ctx = req.ctx
		}
	} else if typ == "CLOSE" {
		if notice := s.doClose(ctx, ws, request, store); notice != "" {
			ws.WriteJSON(nostr.NoticeEnvelope(notice))
		}
		return
	} else if typ == "NEG-OPEN" || typ == "NEG-MSG" || typ == "NEG-CLOSE" {
		// NIP-77 is a lock-step protocol: each NEG-MSG answers the frame the
		// relay just sent, and Reconcile carries state from one frame to the
		// next. Handing these to workers would let a NEG-MSG overtake its
		// NEG-OPEN, or a NEG-CLOSE overtake either, so they run here in
		// receive order like CLOSE does.
		switch typ {
		case "NEG-OPEN":
			s.doNegOpen(ctx, ws, message, store)
		case "NEG-MSG":
			s.doNegMsg(ws, message)
		case "NEG-CLOSE":
			s.doNegClose(ws, message)
		}
		return
	}
	go s.handleParsedMessage(ctx, ws, request, store, typ, req)
}

func (s *Server) handleParsedMessage(ctx context.Context, ws *WebSocket, request []json.RawMessage, store eventstore.Store, typ string, req *subscriptionRequest) {
	var notice string
	defer func() {
		if notice != "" {
			ws.WriteJSON(nostr.NoticeEnvelope(notice))
		}
	}()

	switch typ {
	case "EVENT":
		notice = s.doEvent(ctx, ws, request, store)
	case "COUNT":
		notice = s.doCount(ctx, ws, request, store)
	case "REQ":
		notice = s.doReq(ctx, ws, request, store, req)
	case "AUTH":
		notice = s.doAuth(ctx, ws, request, store)
	default:
		if cwh, ok := s.relay.(CustomWebSocketHandler); ok {
			cwh.HandleUnknownType(ws, typ, request)
		} else {
			notice = "unknown message type " + typ
		}
	}

	// NIP-42 auth challenge
	if strings.HasPrefix(notice, "auth-required:") {
		s.sendAuthChallenge(ws)
	}
}

func (s *Server) sendAuthChallenge(ws *WebSocket) {
	if _, ok := s.relay.(Auther); ok {
		ws.WriteJSON(nostr.AuthEnvelope{Challenge: &ws.challenge})
	}
}

func (s *Server) validateFilterAccess(ws *WebSocket, filter nostr.Filter, allowGiftWrapCheck bool) string {
	if _, ok := s.relay.(Auther); !ok {
		return ""
	}

	if slices.Contains(filter.Kinds, 4) {
		senders := filter.Authors
		receivers, _ := filter.Tags["p"]
		switch {
		case ws.authed == "":
			return "auth-required: this relay requires NIP-42 authentication to serve kind-4 events"
		case len(senders) == 1 && len(receivers) < 2 && senders[0] == ws.authed:
		case len(receivers) == 1 && len(senders) < 2 && receivers[0] == ws.authed:
		default:
			return "restricted: authenticated user does not have authorization for requested filters."
		}
	}

	if allowGiftWrapCheck && slices.Contains(filter.Kinds, nostr.KindGiftWrap) {
		receivers, _ := filter.Tags["p"]
		switch {
		case ws.authed == "":
			return "auth-required: this relay requires NIP-42 authentication to serve gift-wrapped events"
		case len(receivers) == 1 && receivers[0] == ws.authed:
		default:
			return "restricted: authenticated user does not have authorization for requested filters."
		}
	}

	return ""
}

func (s *Server) HandleWebsocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.Log.Errorf("failed to upgrade websocket: %v", err)
		return
	}
	s.clientsMu.Lock()
	s.clients[conn] = struct{}{}
	s.clientsMu.Unlock()
	ticker := time.NewTicker(pingPeriod)

	ip := conn.RemoteAddr().String()
	if realIP := r.Header.Get("X-Forwarded-For"); realIP != "" {
		ip = realIP // possible to be multiple comma separated
	} else if realIP := r.Header.Get("X-Real-Ip"); realIP != "" {
		ip = realIP
	}
	s.Log.Infof("connected from %s", ip)

	ws := challenge(conn)

	if s.options.perConnectionLimiter != nil {
		ws.limiter = rate.NewLimiter(
			s.options.perConnectionLimiter.Limit(),
			s.options.perConnectionLimiter.Burst(),
		)
	}

	ctx, cancel := context.WithCancel(context.Background())

	store := s.relay.Storage(ctx)

	// reader
	go func() {
		defer func() {
			cancel()
			ticker.Stop()
			s.clientsMu.Lock()
			if _, ok := s.clients[conn]; ok {
				conn.Close()
				delete(s.clients, conn)
			}
			s.clientsMu.Unlock()
			s.removeListener(ws)
			ws.clearNegs()
			s.Log.Infof("disconnected from %s", ip)
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
					websocket.CloseNormalClosure,    // 1000
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

			s.handleMessage(ctx, ws, message, store)
		}
	}()

	// writer
	go func() {
		defer func() {
			cancel()
			ticker.Stop()
			conn.Close()
		}()

		for {
			select {
			case <-ticker.C:
				err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(writeWait))
				if err != nil {
					// a client that goes away between keepalives is ordinary churn
					// on a public relay, not a fault: log it at info, not error.
					s.Log.Infof("ping failed for %s, closing websocket: %v", ip, err)
					return
				}
				s.Log.Infof("pinging for %s", ip)
			case <-ctx.Done():
				return
			}
		}
	}()
}

func (s *Server) HandleNIP11(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var info nip11.RelayInformationDocument
	if ifmer, ok := s.relay.(Informationer); ok {
		info = ifmer.GetNIP11InformationDocument()
	} else {
		supportedNIPs := []any{9, 11, 12, 15, 16, 20, 33, 67, 77}
		if _, ok := s.relay.(Auther); ok {
			// NIP-42 authentication gates private direct messages and
			// gift-wrapped events, which relayer handles for Auther relays.
			supportedNIPs = append(supportedNIPs, 17, 42, 59)
		}
		if storage := s.relay.Storage(r.Context()); storage != nil {
			if _, ok = storage.(EventCounter); ok {
				supportedNIPs = append(supportedNIPs, 45)
			}
		}

		info = nip11.RelayInformationDocument{
			Name:          s.relay.Name(),
			Description:   "relay powered by the relayer framework",
			PubKey:        "~",
			Contact:       "~",
			SupportedNIPs: supportedNIPs,
			Software:      "https://github.com/fiatjaf/relayer",
			Version:       "~",
		}
	}

	json.NewEncoder(w).Encode(info)
}
