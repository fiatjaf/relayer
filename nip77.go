// NIP-77 Negentropy set-reconciliation server-side handling.
// The relay replies to NEG-OPEN / NEG-MSG exchanges by iteratively calling
// negentropy.Reconcile on a frozen snapshot of the events matching the filter.
package relayer

import (
	"context"
	"fmt"
	"strings"
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
// instance would race. NEG-* frames are dispatched in receive order and never
// concurrently, so the mutex is only a guard against that changing.
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

// writeNegErr sends a NEG-ERR. go-nostr's nip77.ErrorEnvelope marshals the
// label as "NEG-ERROR", but NIP-77 specifies "NEG-ERR", so the envelope is
// built by hand. go-nostr's own parser matches on a "NEG-ERR" prefix, so
// clients using it still decode this.
//
// Reasons follow the NIP-01 form of a machine-readable single word, a colon,
// and then a human-readable message.
func writeNegErr(ws *WebSocket, subscriptionID string, reason string) {
	ws.WriteJSON([]any{"NEG-ERR", subscriptionID, reason})
}

// doNegOpen starts a negentropy session: snapshot the filtered events into a
// sorted vector and reply with the first Reconcile output. Access is gated by
// the same auth/ReqAccepter checks as REQ so NIP-42/NIP-59 restrictions apply.
func (s *Server) doNegOpen(ctx context.Context, ws *WebSocket, message []byte, store eventstore.Store) {
	env := &nip77.OpenEnvelope{}
	if err := env.UnmarshalJSON(message); err != nil {
		writeNegErr(ws, "", "invalid: failed to decode NEG-OPEN: "+err.Error())
		return
	}
	if env.SubscriptionID == "" {
		writeNegErr(ws, "", "invalid: NEG-OPEN is missing a subscription id")
		return
	}

	// "If a NEG-OPEN is issued for a currently open subscription ID, the
	// existing subscription is first closed." Drop it up front so that the
	// error paths below cannot leave a stale session behind either.
	ws.removeNeg(env.SubscriptionID)

	if reason := s.validateFilterAccess(ws, env.Filter, true); reason != "" {
		// validateFilterAccess already returns NIP-01 formatted reasons
		if strings.HasPrefix(reason, "auth-required:") {
			s.sendAuthChallenge(ws)
		}
		writeNegErr(ws, env.SubscriptionID, reason)
		return
	}

	if accepter, ok := s.relay.(ReqAccepter); ok {
		if !accepter.AcceptReq(ctx, env.SubscriptionID, nostr.Filters{env.Filter}, ws.authed) {
			writeNegErr(ws, env.SubscriptionID, "blocked: NEG-OPEN filter is not accepted")
			return
		}
	}

	events, err := store.QueryEvents(ctx, env.Filter)
	if err != nil {
		writeNegErr(ws, env.SubscriptionID, fmt.Sprintf("error: failed to query events: %v", err))
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
		writeNegErr(ws, env.SubscriptionID, "error: reconcile failed: "+err.Error())
		return
	}
	ws.setNeg(env.SubscriptionID, sess)
	ws.WriteJSON(nip77.MessageEnvelope{SubscriptionID: env.SubscriptionID, Message: output})
}

// doNegMsg advances an open session. Reconcile is stateful per instance, so
// on any error we drop the session — per NIP-77 the subscription is considered
// closed once a NEG-ERR is issued, and the client must reopen from scratch.
func (s *Server) doNegMsg(ws *WebSocket, message []byte) {
	env := &nip77.MessageEnvelope{}
	if err := env.UnmarshalJSON(message); err != nil {
		writeNegErr(ws, "", "invalid: failed to decode NEG-MSG: "+err.Error())
		return
	}
	sess := ws.getNeg(env.SubscriptionID)
	if sess == nil {
		writeNegErr(ws, env.SubscriptionID, "closed: no such negentropy subscription")
		return
	}
	sess.mu.Lock()
	output, err := sess.neg.Reconcile(env.Message)
	sess.mu.Unlock()
	if err != nil {
		ws.removeNeg(env.SubscriptionID)
		writeNegErr(ws, env.SubscriptionID, "error: reconcile failed: "+err.Error())
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
