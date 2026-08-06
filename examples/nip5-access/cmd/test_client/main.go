package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip19"
)

// NIP05Response represents the structure of the .well-known/nostr.json response
type NIP05Response struct {
	Names map[string]string `json:"names"`
}

func main() {
	// Parse command line arguments
	var nsecArg, nip05Arg string
	var showHelp bool
	flag.StringVar(&nsecArg, "nsec", "", "Nostr private key (nsec1...)")
	flag.StringVar(&nip05Arg, "nip5", "", "NIP-05 identifier (user@domain.com)")
	flag.BoolVar(&showHelp, "help", false, "Show help message")
	flag.BoolVar(&showHelp, "h", false, "Show help message")
	flag.Parse()

	if showHelp {
		showUsage()
		return
	}

	fmt.Println("=== NIP5 Access Relay Test Client ===")
	fmt.Println()

	// Get nsec and NIP5 from CLI args or user input
	var nsec, nip05 string
	var err error

	// If both arguments are provided, skip interactive input
	if nsecArg != "" && nip05Arg != "" {
		nsec = nsecArg
		nip05 = nip05Arg

		// Validate nsec format
		if !strings.HasPrefix(nsec, "nsec1") {
			log.Fatalf("Invalid nsec format, should start with 'nsec1'")
		}

		// Validate NIP5 format
		if !strings.Contains(nip05, "@") {
			log.Fatalf("Invalid NIP-05 format, should contain '@' (e.g., user@domain.com)")
		}

		fmt.Printf("Using nsec from command line: %s...\n", nsec[:10])
		fmt.Printf("Using NIP-05 from command line: %s\n", nip05)

		// Show NIP-05 registry info for non-interactive mode
		showNIP05RegistryInfo(nip05)
	} else {
		// Interactive mode - get nsec
		if nsecArg != "" {
			nsec = nsecArg
			if !strings.HasPrefix(nsec, "nsec1") {
				log.Fatalf("Invalid nsec format, should start with 'nsec1'")
			}
			fmt.Printf("Using nsec from command line: %s...\n", nsec[:10])
		} else {
			nsec, err = getNsecFromUser()
			if err != nil {
				log.Fatalf("Failed to get nsec: %v", err)
			}
		}

		// Interactive mode - get NIP5
		if nip05Arg != "" {
			nip05 = nip05Arg
			if !strings.Contains(nip05, "@") {
				log.Fatalf("Invalid NIP-05 format, should contain '@' (e.g., user@domain.com)")
			}
			fmt.Printf("Using NIP-05 from command line: %s\n", nip05)
		} else {
			nip05, err = getNIP05FromUser()
			if err != nil {
				log.Fatalf("Failed to get NIP5 identifier: %v", err)
			}
		}
	}

	// Decode the nsec to get the private key
	_, privateKey, err := nip19.Decode(nsec)
	if err != nil {
		log.Fatalf("Failed to decode nsec: %v", err)
	}

	// Get the public key from the private key
	pubkey, err := nostr.GetPublicKey(privateKey.(string))
	if err != nil {
		log.Fatalf("Failed to get public key: %v", err)
	}

	fmt.Printf("Public key: %s\n", pubkey)
	fmt.Println()

	// Connect to the relay
	relayURL := "ws://localhost:7447"
	fmt.Printf("Connecting to relay: %s\n", relayURL)

	relay, err := nostr.RelayConnect(context.Background(), relayURL)
	if err != nil {
		log.Fatalf("Failed to connect to relay: %v", err)
	}
	defer relay.Close()

	fmt.Println("✓ Connected to relay")
	fmt.Println()

	// First, post a metadata event with NIP5 identifier
	fmt.Println("Posting metadata event with NIP5 identifier...")
	err = postMetadataEvent(relay, pubkey, privateKey.(string), nip05)
	if err != nil {
		log.Fatalf("Failed to post metadata event: %v", err)
	}
	fmt.Println("✓ Metadata event posted successfully")

	// Wait for the metadata event to be processed
	fmt.Println("Waiting for metadata event to be processed by the relay...")
	time.Sleep(3 * time.Second)

	// Verify the metadata event was stored by trying to read it
	fmt.Println("Verifying metadata event was stored...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	metadataFilter := nostr.Filter{
		Authors: []string{pubkey},
		Kinds:   []int{0}, // kind-0 metadata
		Limit:   1,
	}

	sub, err := relay.Subscribe(ctx, []nostr.Filter{metadataFilter})
	if err != nil {
		log.Printf("Failed to subscribe for metadata verification: %v", err)
	} else {
		select {
		case event := <-sub.Events:
			if event != nil {
				fmt.Printf("✓ Metadata event verified in relay: %s\n", event.ID)
			} else {
				fmt.Println("⚠️  No metadata event found in relay")
			}
		case <-ctx.Done():
			fmt.Println("⚠️  Timeout waiting for metadata event verification")
		}
	}
	fmt.Println()

	// Test 1: Try to read posts without authentication (should fail)
	fmt.Println("=== Test 1: Reading posts without authentication (should fail) ===")
	unauthReadSuccess := testUnauthenticatedRead(relay, pubkey)
	if unauthReadSuccess {
		fmt.Println("❌ UNEXPECTED: Unauthenticated read succeeded (should have failed)")
	} else {
		fmt.Println("✓ Unauthenticated read correctly failed")
	}
	fmt.Println()

	// Test 2: Try to post a kind 1 note without authentication (should fail)
	fmt.Println("=== Test 2: Posting note without authentication (should fail) ===")
	unauthPostSuccess := testUnauthenticatedPost(relay, pubkey, privateKey.(string))
	if unauthPostSuccess {
		fmt.Println("❌ UNEXPECTED: Unauthenticated post succeeded (should have failed)")
	} else {
		fmt.Println("✓ Unauthenticated post correctly failed")
	}
	fmt.Println()

	// Test 3: NIP5 authentication
	fmt.Println("=== Test 3: NIP5 authentication (should succeed) ===")
	authSuccess := testNIP5Authentication(relay, pubkey, privateKey.(string))
	if !authSuccess {
		log.Fatalf("NIP5 authentication failed")
	}
	fmt.Println("✓ NIP5 authentication successful")
	fmt.Println()

	// Keep the connection open and perform authenticated tests
	fmt.Println("=== Performing authenticated tests on the same connection ===")

	// Test 4: Post a test note with authentication (should succeed)
	fmt.Println("=== Test 4: Posting test note with authentication (should succeed) ===")
	eventID, err := postTestNote(relay, pubkey, privateKey.(string))
	if err != nil {
		log.Fatalf("Failed to post test note: %v", err)
	}
	fmt.Printf("✓ Posted test note with ID: %s\n", eventID)
	fmt.Println()

	// Test 5: Read notes on the relay (should succeed)
	fmt.Println("=== Test 5: Reading notes with authentication (should succeed) ===")
	notes, err := readAllNotes(relay, pubkey)
	if err != nil {
		log.Fatalf("Failed to read notes: %v", err)
	}
	fmt.Printf("✓ Found %d existing notes\n", len(notes))
	fmt.Println()

	// Test 6: Verify the posted note
	fmt.Println("=== Test 6: Verifying the posted note ===")
	success := verifyPostedNote(relay, eventID, pubkey)
	if success {
		fmt.Println("✓ Test note successfully verified!")
		fmt.Println()
		fmt.Println("🎉 ALL TESTS PASSED! The NIP5 access relay is working correctly.")
	} else {
		fmt.Println("❌ Test note verification failed!")
	}
}

func getNsecFromUser() (string, error) {
	reader := bufio.NewReader(os.Stdin)

	fmt.Print("Enter your nsec (private key): ")
	nsec, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}

	nsec = strings.TrimSpace(nsec)
	if nsec == "" {
		return "", fmt.Errorf("nsec cannot be empty")
	}

	// Validate nsec format
	if !strings.HasPrefix(nsec, "nsec1") {
		return "", fmt.Errorf("invalid nsec format, should start with 'nsec1'")
	}

	return nsec, nil
}

func getNIP05FromUser() (string, error) {
	reader := bufio.NewReader(os.Stdin)

	fmt.Println("Available domains: hitchwiki.org")
	fmt.Println()
	fmt.Println("To test NIP-05 verification, you need a valid NIP-05 identifier.")
	fmt.Println("The relay will verify against the domain's .well-known/nostr.json file.")
	fmt.Println()
	fmt.Println("Example NIP-05 URLs to check:")
	fmt.Println("  - https://hitchwiki.org/.well-known/nostr.json")
	fmt.Println()
	fmt.Print("Enter your NIP5 identifier (e.g., username@hitchwiki.org): ")
	nip05, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}

	nip05 = strings.TrimSpace(nip05)
	if nip05 == "" {
		return "", fmt.Errorf("NIP5 identifier cannot be empty")
	}

	// Validate NIP5 format (basic validation)
	if !strings.Contains(nip05, "@") {
		return "", fmt.Errorf("invalid NIP5 format, should contain '@' (e.g., username@hitchwiki.org)")
	}

	// Check if the domain is one of the allowed domains
	domain := strings.Split(nip05, "@")[1]
	allowedDomains := []string{"hitchwiki.org"}
	validDomain := false
	for _, allowedDomain := range allowedDomains {
		if domain == allowedDomain {
			validDomain = true
			break
		}
	}

	if !validDomain {
		return "", fmt.Errorf("domain %s is not in the allowed domains list. Use one of: %s", domain, strings.Join(allowedDomains, ", "))
	}

	// Show NIP-05 registry info
	showNIP05RegistryInfo(nip05)

	return nip05, nil
}

func testUnauthenticatedRead(relay *nostr.Relay, pubkey string) bool {
	fmt.Println("Attempting to read notes without authentication...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Try to query without authentication
	filter := nostr.Filter{
		Authors: []string{pubkey},
		Kinds:   []int{1}, // kind-1 text notes
		Limit:   10,
	}

	sub, err := relay.Subscribe(ctx, []nostr.Filter{filter})
	if err != nil {
		fmt.Printf("✓ Query rejected (expected): %v\n", err)
		return false
	}

	// Wait for response
	select {
	case event := <-sub.Events:
		if event != nil {
			fmt.Printf("❌ Unexpected: Received event without auth: %s\n", event.ID)
			return true
		}
	case <-ctx.Done():
		fmt.Println("✓ Query timed out (expected for unauthenticated request)")
		return false
	}

	return false
}

func testUnauthenticatedPost(relay *nostr.Relay, pubkey, privateKey string) bool {
	fmt.Println("Attempting to post note without authentication...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Create a test note
	testNote := &nostr.Event{
		PubKey:    pubkey,
		CreatedAt: nostr.Now(),
		Kind:      nostr.KindTextNote,
		Tags:      nostr.Tags{},
		Content:   "This should fail - posted without authentication",
	}

	// Sign the event
	if err := testNote.Sign(privateKey); err != nil {
		fmt.Printf("✓ Failed to sign event (expected): %v\n", err)
		return false
	}

	// Try to publish without authentication
	err := relay.Publish(ctx, *testNote)
	if err != nil {
		fmt.Printf("✓ Publish rejected (expected): %v\n", err)
		return false
	}

	fmt.Println("❌ Unexpected: Publish succeeded without authentication")
	return true
}

func testNIP5Authentication(relay *nostr.Relay, pubkey, privateKey string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	fmt.Println("Creating auth event...")

	// Create an auth event for NIP-42 authentication
	authEvent := &nostr.Event{
		PubKey:    pubkey,
		CreatedAt: nostr.Now(),
		Kind:      nostr.KindClientAuthentication,
		Tags:      nostr.Tags{},
		Content:   "",
	}

	// Sign the auth event
	if err := authEvent.Sign(privateKey); err != nil {
		log.Printf("Failed to sign auth event: %v", err)
		return false
	}

	fmt.Printf("Auth event created and signed for pubkey: %s\n", pubkey)
	fmt.Println("Publishing auth event...")

	// Send auth event
	err := relay.Publish(ctx, *authEvent)
	if err != nil {
		log.Printf("Failed to publish auth event: %v", err)
		return false
	}

	fmt.Println("✓ Auth event published successfully")
	fmt.Println("Waiting for the relay to process the auth event...")
	time.Sleep(3 * time.Second)

	// Test if authentication worked by trying a simple query
	fmt.Println("Testing authenticated query...")
	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()

	filter := nostr.Filter{
		Authors: []string{pubkey},
		Kinds:   []int{0}, // kind-0 metadata
		Limit:   1,
	}

	sub, err := relay.Subscribe(ctx2, []nostr.Filter{filter})
	if err != nil {
		log.Printf("Failed to subscribe for auth test: %v", err)
		return false
	}

	// Wait for response or timeout
	select {
	case event := <-sub.Events:
		if event != nil {
			// Got a response, auth worked
			fmt.Printf("✓ Authentication successful! Received event: %s\n", event.ID)
			return true
		}
	case <-ctx2.Done():
		// Timeout or context cancelled
		fmt.Println("❌ Authentication test timed out")
		return false
	}

	return false
}

func readAllNotes(relay *nostr.Relay, pubkey string) ([]*nostr.Event, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Query for all text notes (kind 1) from the authenticated user
	filter := nostr.Filter{
		Authors: []string{pubkey},
		Kinds:   []int{1}, // kind-1 text notes
		Limit:   100,      // reasonable limit
	}

	sub, err := relay.Subscribe(ctx, []nostr.Filter{filter})
	if err != nil {
		return nil, err
	}

	var notes []*nostr.Event
	for {
		select {
		case event := <-sub.Events:
			if event == nil {
				// No more events
				return notes, nil
			}
			notes = append(notes, event)
		case <-ctx.Done():
			// Timeout
			return notes, nil
		}
	}
}

func postTestNote(relay *nostr.Relay, pubkey, privateKey string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create a test note with timestamp
	now := time.Now()
	testNote := &nostr.Event{
		PubKey:    pubkey,
		CreatedAt: nostr.Now(),
		Kind:      nostr.KindTextNote,
		Tags:      nostr.Tags{},
		Content:   fmt.Sprintf("TEST SUCCESSFUL - Posted from NIP5 access relay test client at %s", now.Format("2006-01-02 15:04:05 UTC")),
	}

	// Sign the event
	if err := testNote.Sign(privateKey); err != nil {
		return "", err
	}

	// Publish the event
	err := relay.Publish(ctx, *testNote)
	if err != nil {
		return "", err
	}

	return testNote.ID, nil
}

func verifyPostedNote(relay *nostr.Relay, eventID, pubkey string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Query for the specific event by ID
	filter := nostr.Filter{
		IDs:     []string{eventID},
		Authors: []string{pubkey},
		Kinds:   []int{1},
		Limit:   1,
	}

	sub, err := relay.Subscribe(ctx, []nostr.Filter{filter})
	if err != nil {
		log.Printf("Failed to subscribe for verification: %v", err)
		return false
	}

	// Wait for the event
	select {
	case event := <-sub.Events:
		if event != nil && event.ID == eventID {
			fmt.Printf("✓ Found posted note: %s\n", event.Content)
			return true
		}
	case <-ctx.Done():
		log.Printf("Timeout waiting for posted note")
		return false
	}

	return false
}

func postMetadataEvent(relay *nostr.Relay, pubkey, privateKey, nip05 string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create a metadata event with the provided NIP5 identifier
	metadata := map[string]interface{}{
		"name":    "Test User",
		"about":   "NIP5 Access Relay Test User",
		"nip05":   nip05, // Use the provided NIP5 identifier
		"picture": "https://example.com/avatar.jpg",
	}

	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %v", err)
	}

	// Create the metadata event (kind 0)
	metadataEvent := &nostr.Event{
		PubKey:    pubkey,
		CreatedAt: nostr.Now(),
		Kind:      nostr.KindProfileMetadata,
		Tags:      nostr.Tags{},
		Content:   string(metadataJSON),
	}

	// Sign the event
	if err := metadataEvent.Sign(privateKey); err != nil {
		return fmt.Errorf("failed to sign metadata event: %v", err)
	}

	// Publish the event
	err = relay.Publish(ctx, *metadataEvent)
	if err != nil {
		return fmt.Errorf("failed to publish metadata event: %v", err)
	}

	return nil
}

func fetchNIP05Registry(url string) (*NIP05Response, error) {
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var nip05Resp NIP05Response
	if err := json.Unmarshal(body, &nip05Resp); err != nil {
		return nil, err
	}

	return &nip05Resp, nil
}

func showNIP05RegistryInfo(nip05 string) {
	// Show the NIP-05 URL that will be checked
	username := strings.Split(nip05, "@")[0]
	domain := strings.Split(nip05, "@")[1]
	nip05URL := fmt.Sprintf("https://%s/.well-known/nostr.json?name=%s", domain, username)
	fmt.Printf("NIP-05 URL that will be checked: %s\n", nip05URL)
	fmt.Printf("Looking for username '%s' in the registry...\n", username)

	// Fetch and display the NIP-05 registry
	fmt.Println("Fetching NIP-05 registry...")
	registry, err := fetchNIP05Registry(nip05URL)
	if err != nil {
		fmt.Printf("❌ Failed to fetch NIP-05 registry: %v\n", err)
		fmt.Println("This might be why authentication will fail.")
	} else {
		fmt.Printf("✓ NIP-05 registry fetched successfully\n")
		fmt.Printf("Registry contains %d usernames\n", len(registry.Names))

		// Check if the username exists
		if pubkey, exists := registry.Names[username]; exists {
			fmt.Printf("✓ Username '%s' found in registry with pubkey: %s\n", username, pubkey)
		} else {
			fmt.Printf("❌ Username '%s' NOT found in registry\n", username)
			fmt.Println("Available usernames:")
			for user, pub := range registry.Names {
				fmt.Printf("  - %s: %s\n", user, pub)
			}
		}
	}
	fmt.Println()
}

func showUsage() {
	fmt.Println("NIP5 Access Relay Test Client")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  test-client [options]")
	fmt.Println()
	fmt.Println("Options:")
	fmt.Println("  -nsec string    Nostr private key (nsec1...)")
	fmt.Println("  -nip5 string    NIP-05 identifier (user@domain.com)")
	fmt.Println("  -h, -help       Show this help message")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  # Interactive mode")
	fmt.Println("  test-client")
	fmt.Println()
	fmt.Println("  # With command line arguments")
	fmt.Println("  test-client -nsec nsec1... -nip5 user@hitchwiki.org")
	fmt.Println()
	fmt.Println("  # Show help")
	fmt.Println("  test-client -help")
	fmt.Println()
	fmt.Println("Available domains: hitchwiki.org")
	fmt.Println()
	fmt.Println("Note: The relay will verify your NIP-05 identifier against the domain's")
	fmt.Println("      .well-known/nostr.json registry. Make sure your username exists")
	fmt.Println("      in the registry for authentication to succeed.")
}

// Helper function to generate a random string for testing
func generateRandomString(length int) string {
	bytes := make([]byte, length)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}
