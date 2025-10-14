# NIP5 Access Relay Test Client

This directory contains the test client for the NIP5 Access Relay. The test client performs comprehensive validation of the relay's security and functionality.

## Features

- **Interactive and CLI modes**: Can be run interactively or with command-line arguments
- **Comprehensive testing**: 6-step validation including security and functionality tests
- **NIP-05 verification**: Fetches and validates against domain registries
- **Persistent authentication**: Maintains connection state across tests
- **Debug output**: Detailed logging for troubleshooting

## Quick Start

```bash
# Interactive mode
make test-client

# CLI mode
./test-client -nsec nsec1... -nip5 user@example.org

# Show help
./test-client -help
```

## Test Flow

The test client performs the following validation:

1. **Setup**: Input validation, NIP-05 registry fetching, relay connection, metadata posting
2. **Test 1**: Unauthenticated read (should fail)
3. **Test 2**: Unauthenticated post (should fail)
4. **Test 3**: NIP-42 authentication (should succeed)
5. **Test 4**: Authenticated post with timestamp (should succeed)
6. **Test 5**: Authenticated read (should succeed)
7. **Test 6**: Note verification (should succeed)

## Usage

### Interactive Mode
```bash
make test-client
```

### CLI Mode
```bash
# With arguments
./test-client -nsec nsec1... -nip5 user@example.org

# Show help
./test-client -help
```

### Using Make
```bash
# Interactive
make test-client

# With environment variables
make test-client-cli NSEC='nsec1...' NIP5='user@example.org'

# Show help
make test-client-help
```

## Prerequisites

- NIP5 access relay must be running
- Valid nsec (private key) with NIP-05 identifier from allowed domain
- Relay configured with your domain in `ALLOWED_DOMAINS`

## Expected Output

```
=== NIP5 Access Relay Test Client ===

Enter your nsec (private key): nsec1...
Enter your NIP5 identifier (e.g., user@example.com): test@example.com
Public key: npub1...

Connecting to relay: ws://localhost:7447
✓ Connected to relay

Posting metadata event with NIP5 identifier...
✓ Metadata event posted successfully

=== Test 1: Reading posts without authentication (should fail) ===
✓ Unauthenticated read correctly failed

=== Test 2: Posting note without authentication (should fail) ===
✓ Unauthenticated post correctly failed

=== Test 3: NIP5 authentication (should succeed) ===
✓ NIP5 authentication successful

=== Performing authenticated tests on the same connection ===

=== Test 4: Posting test note with authentication (should succeed) ===
✓ Posted test note with ID: def456...

=== Test 5: Reading notes with authentication (should succeed) ===
✓ Found 1 existing notes

=== Test 6: Verifying the posted note ===
✓ Test note successfully verified!

🎉 ALL TESTS PASSED! The NIP5 access relay is working correctly.
```

## Troubleshooting

- **Connection Failed**: Ensure relay is running on `ws://localhost:7447`
- **Authentication Failed**: Verify NIP-05 identifier exists in domain registry
- **Domain Not Allowed**: Check `ALLOWED_DOMAINS` configuration
- **Metadata Not Found**: Ensure kind-0 event was posted successfully