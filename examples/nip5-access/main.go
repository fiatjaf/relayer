package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/fiatjaf/eventstore"
	"github.com/fiatjaf/eventstore/postgresql"
	"github.com/fiatjaf/relayer/v2"
	"github.com/kelseyhightower/envconfig"
	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip11"
)

type Relay struct {
	PostgresDatabase string `envconfig:"POSTGRESQL_DATABASE" required:"true"`
	RelayURL         string `envconfig:"RELAY_URL" default:"wss://localhost:7447"`
	AllowedDomains   string `envconfig:"ALLOWED_DOMAINS" required:"true"`

	storage        *postgresql.PostgresBackend
	allowedDomains []string
	// Track NIP-05 verified pubkeys (permanently verified)
	nip05VerifiedPubkeys map[string]bool
	// Cache for NIP-05 domain lookups
	nip05Cache map[string]nip05CacheEntry
	// Mutex for thread-safe access to maps
	mu sync.RWMutex
}

// Ensure Relay implements CustomAuther interface
var _ relayer.CustomAuther = (*Relay)(nil)

type nip05CacheEntry struct {
	Data      map[string]string
	ExpiresAt time.Time
}

func (r *Relay) Name() string {
	return "NIP5AccessRelay"
}

func (r *Relay) Storage(ctx context.Context) eventstore.Store {
	return r.storage
}

func (r *Relay) Init() error {
	// Parse allowed domains from comma-separated string
	log.Printf("Raw ALLOWED_DOMAINS: '%s'", r.AllowedDomains)
	r.allowedDomains = strings.Split(r.AllowedDomains, ",")
	for i, domain := range r.allowedDomains {
		r.allowedDomains[i] = strings.TrimSpace(domain)
	}
	log.Printf("Parsed allowed domains: %v", r.allowedDomains)

	// Initialize maps
	r.nip05VerifiedPubkeys = make(map[string]bool)
	r.nip05Cache = make(map[string]nip05CacheEntry)

	return nil
}

// ServiceURL implements the Auther interface for NIP-42 authentication
func (r *Relay) ServiceURL() string {
	return r.RelayURL
}

// Authenticate implements the Auther interface for NIP-42 authentication
func (r *Relay) Authenticate(ctx context.Context, evt *nostr.Event) (bool, string) {
	fmt.Printf("DEBUG: Authenticate called for event %s from pubkey %s\n", evt.ID, evt.PubKey)

	// Verify the event is a valid authentication event
	if evt.Kind != nostr.KindClientAuthentication {
		fmt.Printf("DEBUG: Not an authentication event (kind %d)\n", evt.Kind)
		return false, "not an authentication event"
	}

	// Validate AUTH event timing (should be recent)
	now := nostr.Now()
	if evt.CreatedAt < now-600 || evt.CreatedAt > now+60 {
		fmt.Printf("DEBUG: AUTH event timestamp too old or too far in future\n")
		return false, "auth event timestamp invalid"
	}

	// Check if this pubkey is already NIP-05 verified
	r.mu.RLock()
	isVerified := r.nip05VerifiedPubkeys[evt.PubKey]
	r.mu.RUnlock()

	if isVerified {
		fmt.Printf("DEBUG: Pubkey %s is already NIP-05 verified\n", evt.PubKey)
		return true, ""
	}

	// Verify the event author has a valid NIP-5 from allowed domains
	valid, reason := verifyNIP05WithCache(ctx, r, evt.PubKey, r.allowedDomains)
	if !valid {
		fmt.Printf("DEBUG: Authentication failed for %s: %s\n", evt.PubKey, reason)
		return false, reason
	}

	// Mark this pubkey as NIP-05 verified
	r.mu.Lock()
	r.nip05VerifiedPubkeys[evt.PubKey] = true
	r.mu.Unlock()
	fmt.Printf("DEBUG: Authentication successful for %s - marked as NIP-05 verified\n", evt.PubKey)
	return true, ""
}

// AcceptEvent implements the Relay interface - verifies NIP-5 before accepting events
func (r *Relay) AcceptEvent(ctx context.Context, evt *nostr.Event) (bool, string) {
	fmt.Printf("DEBUG: AcceptEvent called for kind %d from pubkey %s\n", evt.Kind, evt.PubKey)

	// Block events that are too large
	jsonb, _ := json.Marshal(evt)
	if len(jsonb) > 100000 {
		return false, "event is too large"
	}

	// Allow kind-0 (profile metadata) events without authentication
	// This allows users to post their metadata first, which is needed for NIP-5 verification
	if evt.Kind == nostr.KindProfileMetadata {
		fmt.Printf("DEBUG: Allowing kind-0 metadata event from %s (no auth required)\n", evt.PubKey)
		return true, ""
	}

	// Allow kind-22242 (auth events) without authentication - they're part of the NIP-42 flow
	if evt.Kind == nostr.KindClientAuthentication {
		fmt.Printf("DEBUG: Allowing kind-22242 auth event from %s (part of NIP-42 flow)\n", evt.PubKey)
		return true, ""
	}

	// Check if this pubkey is NIP-05 verified (can always post)
	r.mu.RLock()
	isNIP05Verified := r.nip05VerifiedPubkeys[evt.PubKey]
	r.mu.RUnlock()

	if isNIP05Verified {
		fmt.Printf("DEBUG: Pubkey %s is NIP-05 verified - allowing kind %d event\n", evt.PubKey, evt.Kind)
		return true, ""
	}

	// For all other event kinds, require NIP-05 verification
	fmt.Printf("DEBUG: Kind %d event requires NIP-05 verification for %s\n", evt.Kind, evt.PubKey)
	return false, "NIP-05 verification required for this event kind"
}

// AcceptReq implements the ReqAccepter interface - verifies NIP-5 before serving queries
func (r *Relay) AcceptReq(ctx context.Context, id string, filters nostr.Filters, authedPubkey string) bool {
	fmt.Printf("DEBUG: AcceptReq called with authedPubkey: %s\n", authedPubkey)

	// If no authentication, deny access
	if authedPubkey == "" {
		fmt.Printf("DEBUG: No authentication provided, denying request\n")
		return false
	}

	// If we have an authenticated pubkey, allow the request
	// The authentication was already verified in the Authenticate function
	fmt.Printf("DEBUG: Pubkey %s is authenticated - allowing request\n", authedPubkey)
	return true
}

// GetNIP11InformationDocument implements the Informationer interface
func (r *Relay) GetNIP11InformationDocument() nip11.RelayInformationDocument {
	return nip11.RelayInformationDocument{
		Name:          "NIP-5 Access Relay",
		Description:   "A Nostr relay that allows profile metadata (kind-0) events from anyone, but requires valid NIP-5 identifiers from allowed domains for all other events",
		PubKey:        "~",
		Contact:       "~",
		SupportedNIPs: []any{1, 9, 11, 12, 15, 16, 20, 33, 42},
		Software:      "https://github.com/fiatjaf/relayer",
		Version:       "1.0.0",
	}
}

func main() {
	r := Relay{}
	if err := envconfig.Process("", &r); err != nil {
		log.Fatalf("failed to read from env: %v", err)
		return
	}

	// Initialize PostgreSQL storage
	r.storage = &postgresql.PostgresBackend{DatabaseURL: r.PostgresDatabase}

	// Create and start the server
	server, err := relayer.NewServer(&r)
	if err != nil {
		log.Fatalf("failed to create server: %v", err)
	}

	log.Printf("Starting NIP-5 Access Relay on 0.0.0.0:7447")
	log.Printf("Relay URL: %s", r.RelayURL)
	log.Printf("PostgreSQL Database: %s", r.PostgresDatabase)
	log.Printf("Allowed Domains: %s", r.AllowedDomains)

	if err := server.Start("0.0.0.0", 7447); err != nil {
		log.Fatalf("server terminated: %v", err)
	}
}

// verifyNIP05WithCache performs NIP-05 verification with caching
func verifyNIP05WithCache(ctx context.Context, relay *Relay, pubkey string, allowedDomains []string) (bool, string) {
	// First try the original verification
	valid, reason := verifyNIP05(ctx, relay.storage, pubkey, allowedDomains)

	// If successful, cache the result
	if valid {
		relay.mu.Lock()
		relay.nip05VerifiedPubkeys[pubkey] = true
		relay.mu.Unlock()
	}

	return valid, reason
}
