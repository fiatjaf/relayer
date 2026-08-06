package main

import (
	"bufio"
	"context"
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

	fiatjafNostr "fiatjaf.com/nostr"
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

	// Convert to fiatjaf types
	fiatjafPrivateKey := fiatjafNostr.SecretKey{}
	privateKeyBytes, err := hex.DecodeString(privateKey.(string))
	if err != nil {
		log.Fatalf("Failed to decode private key: %v", err)
	}
	copy(fiatjafPrivateKey[:], privateKeyBytes)

	// Get the public key from the private key
	fiatjafPubkey := fiatjafNostr.GetPublicKey(fiatjafPrivateKey)
	pubkey := hex.EncodeToString(fiatjafPubkey[:])

	fmt.Printf("Public key: %s\n", pubkey)
	fmt.Printf("NIP-05 identifier: %s\n", nip05)
	fmt.Println()

	// Connect to the relay using fiatjaf
	relayURL := "ws://localhost:7447"
	fmt.Printf("Connecting to relay: %s\n", relayURL)

	relay, err := fiatjafNostr.RelayConnect(context.Background(), relayURL, fiatjafNostr.RelayOptions{})
	if err != nil {
		log.Fatalf("Failed to connect to relay: %v", err)
	}
	defer relay.Close()

	fmt.Println("✓ Connected to relay")
	fmt.Println()

	// Run the tests
	success := runTestsFiatjaf(relay, fiatjafPubkey, fiatjafPrivateKey, nip05)
	if success {
		fmt.Println("\n🎉 All tests passed!")
	} else {
		fmt.Println("\n❌ Some tests failed!")
		os.Exit(1)
	}
}

func runTestsFiatjaf(relay *fiatjafNostr.Relay, pubkey fiatjafNostr.PubKey, privateKey fiatjafNostr.SecretKey, nip05 string) bool {
	fmt.Println("=== Running NIP5 Access Relay Tests ===")
	fmt.Println()

	// Test 1: Try to read posts without authentication (should fail)
	fmt.Println("=== Test 1: Reading posts without authentication (should fail) ===")
	if !testUnauthenticatedRead(relay, pubkey) {
		fmt.Println("✓ Unauthenticated read correctly failed")
	} else {
		fmt.Println("❌ UNEXPECTED: Unauthenticated read succeeded (should have failed)")
		return false
	}
	fmt.Println()

	// Test 2: Posting kind-0 metadata without authentication (should succeed)
	fmt.Println("=== Test 2: Posting kind-0 metadata without authentication (should succeed) ===")
	if testUnauthenticatedKind0Post(relay, pubkey, privateKey, nip05) {
		fmt.Println("✓ Unauthenticated kind-0 post correctly succeeded")
	} else {
		fmt.Println("❌ UNEXPECTED: Unauthenticated kind-0 post failed (should have succeeded)")
		return false
	}
	fmt.Println()

	// Test 3: Posting kind-1 note without authentication (should fail)
	fmt.Println("=== Test 3: Posting kind-1 note without authentication (should fail) ===")
	if !testUnauthenticatedKind1Post(relay, pubkey, privateKey) {
		fmt.Println("✓ Unauthenticated kind-1 post correctly failed")
	} else {
		fmt.Println("❌ UNEXPECTED: Unauthenticated kind-1 post succeeded (should have failed)")
		return false
	}
	fmt.Println()

	// Test 4: NIP5 authentication (should succeed)
	fmt.Println("=== Test 4: NIP5 authentication (should succeed) ===")
	if testNIP5Authentication(relay, pubkey, privateKey) {
		fmt.Println("✓ NIP5 authentication succeeded")
	} else {
		fmt.Println("❌ NIP5 authentication failed")
		return false
	}
	fmt.Println()

	// Test 5: Posting kind-1 note after authentication (should succeed)
	fmt.Println("=== Test 5: Posting kind-1 note after authentication (should succeed) ===")
	if testAuthenticatedKind1Post(relay, pubkey, privateKey) {
		fmt.Println("✓ Authenticated kind-1 post succeeded")
	} else {
		fmt.Println("❌ Authenticated kind-1 post failed")
		return false
	}
	fmt.Println()

	// Test 6: Reading notes after authentication (should succeed)
	fmt.Println("=== Test 6: Reading notes after authentication (should succeed) ===")
	if testAuthenticatedRead(relay, pubkey) {
		fmt.Println("✓ Authenticated read succeeded")
	} else {
		fmt.Println("❌ Authenticated read failed")
		return false
	}
	fmt.Println()

	return true
}

func testUnauthenticatedRead(relay *fiatjafNostr.Relay, pubkey fiatjafNostr.PubKey) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	fmt.Println("Attempting to read posts without authentication...")

	filter := fiatjafNostr.Filter{
		Authors: []fiatjafNostr.PubKey{pubkey},
		Kinds:   []fiatjafNostr.Kind{fiatjafNostr.KindProfileMetadata},
		Limit:   1,
	}

	sub, err := relay.Subscribe(ctx, filter, fiatjafNostr.SubscriptionOptions{})
	if err != nil {
		fmt.Printf("✓ Read rejected (expected): %v\n", err)
		return false
	}

	// Wait for response or timeout
	select {
	case <-sub.Events:
		// Got a response, which means it succeeded (unexpected)
		fmt.Println("❌ Unexpected: Read succeeded without authentication")
		return true
	case <-ctx.Done():
		// Timeout or context cancelled (expected)
		fmt.Println("✓ Read timed out (expected for unauthenticated request)")
		return false
	}
}

func testUnauthenticatedKind0Post(relay *fiatjafNostr.Relay, pubkey fiatjafNostr.PubKey, privateKey fiatjafNostr.SecretKey, nip05 string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	fmt.Println("Attempting to post kind-0 metadata without authentication...")

	// Create a kind-0 metadata event
	event := fiatjafNostr.Event{
		Kind:      fiatjafNostr.KindProfileMetadata,
		CreatedAt: fiatjafNostr.Now(),
		Tags:      fiatjafNostr.Tags{},
		Content:   fmt.Sprintf(`{"name":"Test User","about":"NIP5 Access Relay Test User","picture":"https://example.com/avatar.jpg","nip05":"%s"}`, nip05),
		PubKey:    pubkey,
	}

	// Sign the event
	if err := event.Sign(privateKey); err != nil {
		log.Printf("Failed to sign event: %v", err)
		return false
	}

	// Try to publish the event
	err := relay.Publish(ctx, event)
	if err != nil {
		fmt.Printf("❌ Unexpected: Kind-0 publish failed: %v\n", err)
		return false
	}

	fmt.Println("✓ Kind-0 post succeeded without authentication (expected)")
	return true
}

func testUnauthenticatedKind1Post(relay *fiatjafNostr.Relay, pubkey fiatjafNostr.PubKey, privateKey fiatjafNostr.SecretKey) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	fmt.Println("Attempting to post kind-1 note without authentication...")

	// Create a kind-1 note event
	event := fiatjafNostr.Event{
		Kind:      fiatjafNostr.KindTextNote,
		CreatedAt: fiatjafNostr.Now(),
		Tags:      fiatjafNostr.Tags{},
		Content:   "This is a test note that should be rejected without authentication",
		PubKey:    pubkey,
	}

	// Sign the event
	if err := event.Sign(privateKey); err != nil {
		log.Printf("Failed to sign event: %v", err)
		return false
	}

	// Try to publish the event
	err := relay.Publish(ctx, event)
	if err != nil {
		fmt.Printf("✓ Publish rejected (expected): %v\n", err)
		return false
	}

	fmt.Println("❌ Unexpected: Kind-1 publish succeeded without authentication")
	return true
}

func testNIP5Authentication(relay *fiatjafNostr.Relay, pubkey fiatjafNostr.PubKey, privateKey fiatjafNostr.SecretKey) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	fmt.Println("Testing NIP-42 authentication using fiatjaf library...")

	// Use fiatjaf's built-in Auth method for NIP-42 authentication
	fmt.Println("Performing NIP-42 authentication...")

	err := relay.Auth(ctx, func(ctx context.Context, event *fiatjafNostr.Event) error {
		// Sign the AUTH event with our private key
		return event.Sign(privateKey)
	})

	if err != nil {
		log.Printf("Failed to authenticate: %v", err)
		return false
	}

	fmt.Println("✓ Authentication successful!")

	// Test if authentication worked by trying a query
	fmt.Println("Testing authenticated query...")
	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()

	filter := fiatjafNostr.Filter{
		Authors: []fiatjafNostr.PubKey{pubkey},
		Kinds:   []fiatjafNostr.Kind{fiatjafNostr.KindProfileMetadata}, // kind-0 metadata
		Limit:   1,
	}

	sub, err := relay.Subscribe(ctx2, filter, fiatjafNostr.SubscriptionOptions{})
	if err != nil {
		log.Printf("Failed to subscribe after auth: %v", err)
		return false
	}

	// Wait for response or timeout
	select {
	case event := <-sub.Events:
		// Got a response, auth worked
		fmt.Printf("✓ Query successful! Received event: %s\n", event.ID)
		return true
	case <-ctx2.Done():
		// Timeout or context cancelled
		fmt.Println("❌ Query timed out")
		return false
	}
}

func testAuthenticatedKind1Post(relay *fiatjafNostr.Relay, pubkey fiatjafNostr.PubKey, privateKey fiatjafNostr.SecretKey) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	fmt.Println("Attempting to post kind-1 note after authentication...")

	// Create a kind-1 note event with timestamp
	event := fiatjafNostr.Event{
		Kind:      fiatjafNostr.KindTextNote,
		CreatedAt: fiatjafNostr.Now(),
		Tags:      fiatjafNostr.Tags{},
		Content:   fmt.Sprintf("TEST SUCCESSFUL - %s", time.Now().Format(time.RFC3339)),
		PubKey:    pubkey,
	}

	// Sign the event
	if err := event.Sign(privateKey); err != nil {
		log.Printf("Failed to sign event: %v", err)
		return false
	}

	// Try to publish the event
	err := relay.Publish(ctx, event)
	if err != nil {
		log.Printf("Failed to publish authenticated note: %v", err)
		return false
	}

	fmt.Println("✓ Authenticated kind-1 post succeeded")
	return true
}

func testAuthenticatedRead(relay *fiatjafNostr.Relay, pubkey fiatjafNostr.PubKey) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	fmt.Println("Attempting to read notes after authentication...")

	filter := fiatjafNostr.Filter{
		Authors: []fiatjafNostr.PubKey{pubkey},
		Kinds:   []fiatjafNostr.Kind{fiatjafNostr.KindTextNote},
		Limit:   10,
	}

	sub, err := relay.Subscribe(ctx, filter, fiatjafNostr.SubscriptionOptions{})
	if err != nil {
		log.Printf("Failed to subscribe: %v", err)
		return false
	}

	// Wait for response or timeout
	select {
	case event := <-sub.Events:
		// Got a response, auth worked
		fmt.Printf("✓ Read successful! Received event: %s\n", event.ID)
		return true
	case <-ctx.Done():
		// Timeout or context cancelled
		fmt.Println("❌ Read timed out")
		return false
	}
}

func getNsecFromUser() (string, error) {
	fmt.Print("Enter your nsec (private key): ")
	reader := bufio.NewReader(os.Stdin)
	nsec, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(nsec), nil
}

func getNIP05FromUser() (string, error) {
	fmt.Print("Enter your NIP-05 identifier (e.g., user@domain.com): ")
	reader := bufio.NewReader(os.Stdin)
	nip05, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(nip05), nil
}

func showNIP05RegistryInfo(nip05 string) {
	parts := strings.Split(nip05, "@")
	if len(parts) != 2 {
		fmt.Printf("Invalid NIP-05 format: %s\n", nip05)
		return
	}

	username := parts[0]
	domain := parts[1]
	nip05URL := fmt.Sprintf("https://%s/.well-known/nostr.json?name=%s", domain, username)

	fmt.Printf("NIP-05 URL: %s\n", nip05URL)
	fmt.Println("Fetching NIP-05 registry...")

	// Fetch the NIP-05 registry
	resp, err := http.Get(nip05URL)
	if err != nil {
		fmt.Printf("Failed to fetch NIP-05 registry: %v\n", err)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("Failed to read NIP-05 registry response: %v\n", err)
		return
	}

	var nip05Resp NIP05Response
	if err := json.Unmarshal(body, &nip05Resp); err != nil {
		fmt.Printf("Failed to parse NIP-05 registry response: %v\n", err)
		return
	}

	fmt.Printf("NIP-05 registry response: %+v\n", nip05Resp)
	fmt.Println()
}

func showUsage() {
	fmt.Println("NIP5 Access Relay Test Client")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  ./test-client [options]")
	fmt.Println()
	fmt.Println("Options:")
	fmt.Println("  -nsec string")
	fmt.Println("        Nostr private key (nsec1...)")
	fmt.Println("  -nip5 string")
	fmt.Println("        NIP-05 identifier (user@domain.com)")
	fmt.Println("  -h, -help")
	fmt.Println("        Show this help message")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  ./test-client")
	fmt.Println("        Interactive mode - prompts for nsec and NIP-05")
	fmt.Println()
	fmt.Println("  ./test-client -nsec nsec1... -nip5 user@domain.com")
	fmt.Println("        Non-interactive mode - uses provided values")
	fmt.Println()
	fmt.Println("This test client will:")
	fmt.Println("1. Try to read posts without authentication (should fail)")
	fmt.Println("2. Post kind-0 metadata without authentication (should succeed)")
	fmt.Println("3. Post kind-1 note without authentication (should fail)")
	fmt.Println("4. Perform NIP-42 authentication (should succeed)")
	fmt.Println("5. Post kind-1 note after authentication (should succeed)")
	fmt.Println("6. Read notes after authentication (should succeed)")
}
