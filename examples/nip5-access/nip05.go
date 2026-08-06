package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/fiatjaf/eventstore"
	"github.com/nbd-wtf/go-nostr"
)

// NIP05Response represents the structure of the .well-known/nostr.json response
type NIP05Response struct {
	Names map[string]string `json:"names"`
}

// verifyNIP05 verifies that a pubkey has a valid NIP-05 identifier from allowed domains
func verifyNIP05(ctx context.Context, store eventstore.Store, pubkey string, allowedDomains []string) (bool, string) {
	fmt.Printf("DEBUG: Starting NIP-05 verification for pubkey: %s\n", pubkey)

	// First, get the user's kind-0 metadata to extract their NIP-05
	metadata, err := getUserMetadata(ctx, store, pubkey)
	if err != nil {
		fmt.Printf("DEBUG: Failed to get user metadata: %v\n", err)
		return false, fmt.Sprintf("failed to get user metadata: %v", err)
	}

	fmt.Printf("DEBUG: Retrieved metadata: %+v\n", metadata)

	// Extract NIP-05 from metadata
	nip05, ok := metadata["nip05"].(string)
	if !ok || nip05 == "" {
		fmt.Printf("DEBUG: No NIP-05 identifier found in metadata\n")
		return false, "user has no NIP-05 identifier"
	}

	fmt.Printf("DEBUG: Found NIP-05 identifier: %s\n", nip05)

	// Check if it's from an allowed domain
	domain := extractDomain(nip05)
	if domain == "" {
		fmt.Printf("DEBUG: Invalid NIP-05 format, could not extract domain\n")
		return false, "invalid NIP-05 format"
	}

	fmt.Printf("DEBUG: Extracted domain: %s\n", domain)

	// Check if domain is in allowed list
	fmt.Printf("DEBUG: Checking domain '%s' against allowed domains: %v\n", domain, allowedDomains)
	if !isDomainAllowed(domain, allowedDomains) {
		fmt.Printf("DEBUG: Domain not allowed\n")
		return false, fmt.Sprintf("NIP-05 domain not allowed, got: %s", nip05)
	}

	// Extract username from the NIP-05 identifier
	username := strings.TrimSuffix(nip05, "@"+domain)
	if username == "" {
		fmt.Printf("DEBUG: Invalid NIP-05 format, could not extract username\n")
		return false, "invalid NIP-05 format"
	}

	fmt.Printf("DEBUG: Extracted username: %s\n", username)

	// Verify against the domain's .well-known/nostr.json
	fmt.Printf("DEBUG: Verifying against domain: %s\n", domain)
	valid, reason := verifyAgainstDomain(ctx, domain, username, pubkey)
	fmt.Printf("DEBUG: Domain verification result: %v, reason: %s\n", valid, reason)
	return valid, reason
}

// getUserMetadata fetches the user's kind-0 metadata event from storage
func getUserMetadata(ctx context.Context, store eventstore.Store, pubkey string) (map[string]interface{}, error) {
	// Query for kind-0 events from this pubkey
	filter := nostr.Filter{
		Authors: []string{pubkey},
		Kinds:   []int{0}, // kind-0 is profile metadata
		Limit:   1,
	}

	events, err := store.QueryEvents(ctx, filter)
	if err != nil {
		return nil, err
	}

	// Get the first (most recent) metadata event
	select {
	case event := <-events:
		if event == nil {
			return nil, fmt.Errorf("no metadata found for pubkey %s", pubkey)
		}

		// Parse the content JSON
		var metadata map[string]interface{}
		if err := json.Unmarshal([]byte(event.Content), &metadata); err != nil {
			return nil, fmt.Errorf("failed to parse metadata JSON: %v", err)
		}

		return metadata, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// extractDomain extracts the domain from a NIP-05 identifier
func extractDomain(nip05 string) string {
	parts := strings.Split(nip05, "@")
	if len(parts) != 2 {
		return ""
	}
	return parts[1]
}

// isDomainAllowed checks if a domain is in the allowed domains list
func isDomainAllowed(domain string, allowedDomains []string) bool {
	for _, allowedDomain := range allowedDomains {
		if domain == allowedDomain {
			return true
		}
	}
	return false
}

// verifyAgainstDomain verifies the username/pubkey mapping against a domain's NIP-05 registry
func verifyAgainstDomain(ctx context.Context, domain, username, pubkey string) (bool, string) {
	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	// Fetch the .well-known/nostr.json from the domain with username parameter
	url := fmt.Sprintf("https://%s/.well-known/nostr.json?name=%s", domain, username)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return false, fmt.Sprintf("failed to create request: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return false, fmt.Sprintf("failed to fetch NIP-05 verification: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, fmt.Sprintf("%s returned status %d", domain, resp.StatusCode)
	}

	// Read and parse the response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, fmt.Sprintf("failed to read response: %v", err)
	}

	var nip05Resp NIP05Response
	if err := json.Unmarshal(body, &nip05Resp); err != nil {
		return false, fmt.Sprintf("failed to parse NIP-05 response: %v", err)
	}

	// Check if the username maps to the correct pubkey
	verifiedPubkey, exists := nip05Resp.Names[username]
	if !exists {
		return false, fmt.Sprintf("username %s not found in %s NIP-05 registry", username, domain)
	}

	if verifiedPubkey != pubkey {
		return false, fmt.Sprintf("NIP-05 verification failed: username %s maps to %s, expected %s", username, verifiedPubkey, pubkey)
	}

	return true, ""
}
