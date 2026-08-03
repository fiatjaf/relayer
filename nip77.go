// NIP-77 Negentropy set-reconciliation server-side handling.
// The relay replies to NEG-OPEN / NEG-MSG exchanges by iteratively calling
// negentropy.Reconcile on a frozen snapshot of the events matching the filter.
package relayer

import (
	"context"
	"fmt"
	"sync"

	"github.com/fiatjaf/eventstore"
	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip77"
	"github.com/nbd-wtf/go-nostr/nip77/negentropy"
	"github.com/nbd-wtf/go-nostr/nip77/negentropy/storage/vector"
)

// Upper bound on the size of a single NEG-MSG payload. Reconcile coalesces
// further work into subsequent frames when this is reached.
const negFrameSizeLimit = 4096 * 16

// negSession wraps a Negentropy instance with a mutex. Reconcile mutates
// internal state (lastTimestamp in/out), so concurrent calls on the same
// instance would race — a buggy client sending multiple NEG-MSG frames in
// flight would trigger it since handleMessage dispatches each frame in its
// own goroutine.
type negSession struct {
	mu  sync.Mutex
	neg *negentropy.Negentropy
}

func (ws *WebSocket) getNeg(id string) *negSession {
	ws.negsMu.Lock()
	defer ws.negsMu.Unlock()
	if ws.negs == nil {
		return nil
	}
	return ws.negs[id]
}

func (ws *WebSocket) setNeg(id string, sess *negSession) {
	ws.negsMu.Lock()
	defer ws.negsMu.Unlock()
	if ws.negs == nil {
		ws.negs = make(map[string]*negSession)
	}
	ws.negs[id] = sess
}

func (ws *WebSocket) removeNeg(id string) {
	ws.negsMu.Lock()
	defer ws.negsMu.Unlock()
	delete(ws.negs, id)
}

func (ws *WebSocket) clearNegs() {
	ws.negsMu.Lock()
	defer ws.negsMu.Unlock()
	ws.negs = nil
}

// doNegOpen starts a negentropy session: snapshot the filtered events into a
// sorted vector and reply with the first Reconcile output. Access is gated by
// the same auth/ReqAccepter checks as REQ so NIP-42/NIP-59 restrictions apply.
func (s *Server) doNegOpen(ctx context.Context, ws *WebSocket, message []byte, store eventstore.Store) {
	env := &nip77.OpenEnvelope{}
	if err := env.UnmarshalJSON(message); err != nil {
		ws.WriteJSON(nip77.ErrorEnvelope{Reason: "failed to decode NEG-OPEN: " + err.Error()})
		return
	}
	if env.SubscriptionID == "" {
		ws.WriteJSON(nip77.ErrorEnvelope{Reason: "NEG-OPEN missing subscription id"})
		return
	}

	if reason := s.validateFilterAccess(ws, env.Filter, true); reason != "" {
		ws.WriteJSON(nip77.ErrorEnvelope{SubscriptionID: env.SubscriptionID, Reason: reason})
		return
	}

	if accepter, ok := s.relay.(ReqAccepter); ok {
		if !accepter.AcceptReq(ctx, env.SubscriptionID, nostr.Filters{env.Filter}, ws.authed) {
			ws.WriteJSON(nip77.ErrorEnvelope{SubscriptionID: env.SubscriptionID, Reason: "NEG-OPEN filter not accepted"})
			return
		}
	}

	events, err := store.QueryEvents(ctx, env.Filter)
	if err != nil {
		ws.WriteJSON(nip77.ErrorEnvelope{SubscriptionID: env.SubscriptionID, Reason: fmt.Sprintf("failed to query events: %v", err)})
		return
	}

	// Build the storage snapshot. Seal sorts by (createdAt, id) and freezes
	// the set — Reconcile requires a stable ordering across the whole session.
	vec := vector.New()
	if events != nil {
		for evt := range events {
			if s.options.skipEventFunc != nil && s.options.skipEventFunc(evt) {
				continue
			}
			vec.Insert(evt.CreatedAt, evt.ID)
		}
	}
	vec.Seal()

	// Server-side uses Reconcile directly (no Start); Start is for the
	// initiator, which the client already did before sending NEG-OPEN.
	sess := &negSession{neg: negentropy.New(vec, negFrameSizeLimit)}
	sess.mu.Lock()
	output, err := sess.neg.Reconcile(env.Message)
	sess.mu.Unlock()
	if err != nil {
		ws.WriteJSON(nip77.ErrorEnvelope{SubscriptionID: env.SubscriptionID, Reason: "reconcile failed: " + err.Error()})
		return
	}
	ws.setNeg(env.SubscriptionID, sess)
	ws.WriteJSON(nip77.MessageEnvelope{SubscriptionID: env.SubscriptionID, Message: output})
}

// doNegMsg advances an open session. Reconcile is stateful per instance, so
// on any error we drop the session — the client must reopen from scratch.
func (s *Server) doNegMsg(ws *WebSocket, message []byte) {
	env := &nip77.MessageEnvelope{}
	if err := env.UnmarshalJSON(message); err != nil {
		ws.WriteJSON(nip77.ErrorEnvelope{Reason: "failed to decode NEG-MSG: " + err.Error()})
		return
	}
	sess := ws.getNeg(env.SubscriptionID)
	if sess == nil {
		ws.WriteJSON(nip77.ErrorEnvelope{SubscriptionID: env.SubscriptionID, Reason: "unknown subscription"})
		return
	}
	sess.mu.Lock()
	output, err := sess.neg.Reconcile(env.Message)
	sess.mu.Unlock()
	if err != nil {
		ws.removeNeg(env.SubscriptionID)
		ws.WriteJSON(nip77.ErrorEnvelope{SubscriptionID: env.SubscriptionID, Reason: "reconcile failed: " + err.Error()})
		return
	}
	ws.WriteJSON(nip77.MessageEnvelope{SubscriptionID: env.SubscriptionID, Message: output})
}

func (s *Server) doNegClose(ws *WebSocket, message []byte) {
	env := &nip77.CloseEnvelope{}
	if err := env.UnmarshalJSON(message); err != nil {
		return
	}
	ws.removeNeg(env.SubscriptionID)
}
