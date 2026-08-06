# NIP-5 Access Relay

A Nostr relay that allows profile metadata (kind-0) events from anyone, but requires valid NIP-5 identifiers from allowed domains for all other events.

## Features

- Profile metadata (kind-0) events from anyone
- All other events require valid NIP-5 identifiers from allowed domains
- NIP-42 authentication
- PostgreSQL storage
- Real-time NIP-5 verification

## Prerequisites

- Go 1.23 or later
- PostgreSQL database

## Quick Start

1. **Create database:**
```sql
CREATE DATABASE nip5_access_relay;
```

2. **Set environment variables:**
```bash
export POSTGRESQL_DATABASE="postgres://postgres:password@localhost/nip5_access_relay?sslmode=disable"
export ALLOWED_DOMAINS="example.com,example.org"
```

3. **Run:**
```bash
make deps
make run-example
```

## Configuration

| Variable | Description | Required |
|----------|-------------|----------|
| `POSTGRESQL_DATABASE` | PostgreSQL connection string | Yes |
| `RELAY_URL` | Public URL of the relay | No (default: wss://localhost:7447) |
| `ALLOWED_DOMAINS` | Comma-separated list of allowed domains | Yes |

## Development

```bash
# Build and run
make build
make run

# Test
make test

# Test client
make test-client
```

## How It Works

1. Users connect with their Nostr keys (npub/nsec)
2. Relay requires NIP-42 authentication
3. For each event, relay verifies the author has a valid NIP-5 from allowed domains
4. Profile metadata (kind-0) events are allowed from anyone
5. All other events require valid NIP-5 verification



