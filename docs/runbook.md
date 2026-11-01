# Steps to Run

## Prerequisites

| Requirement | Version | Notes |
|---|---|---|
| Go | 1.21+ | Tested with Go 1.27.1 |
| Git | any | For cloning / versioning |
| C compiler (gcc/clang) | any | Required by `go-sqlite3` CGO dependency |
| SQLite | bundled | `go-sqlite3` includes libsqlite3; no system SQLite needed |

Verify Go:
```bash
go version
# go version go1.27.1 linux/amd64
```

## Clone / Setup

```bash
git clone <repo> mem-sys
cd mem-sys
```

The repo already contains:
- `docs/spec.md` — full specification
- `docs/architecture.md` — backend architecture
- `docs/api.md` — REST + MCP API reference
- `docs/runbook.md` — this file
- `docs/url-prefill-form-plan.md` — pre-filled proposal form link feature plan
- `tools/module_pattern_gen.md` — module path pattern generator utility
- `migrations/001_core_mvp.sql` — core MVP SQLite schema
- `migrations/002_idempotency_keys.sql` — idempotency key registry schema
- `vendor/` — vendored Go dependencies (for offline/air-gapped builds)
- `cmd/mem-sys/main.go` — application entrypoint
- `internal/` — store, service, http, mcp, db layers

## Build

```bash
go build -o mem-sys ./cmd/mem-sys
```

Produces a single binary `mem-sys`.

### Air-Gapped / Offline Deployment

The project ships with vendored dependencies (`vendor/`), so you can build on a server with **no internet access**.

**On the air-gapped server:**

```bash
# Build using only the vendored dependencies — zero downloads needed
go build -mod=vendor -o mem-sys ./cmd/mem-sys
```

**Requirement — C compiler (gcc/clang):** The project uses `mattn/go-sqlite3`, which is a CGO package. A C compiler must be present on the machine where you build:

```bash
# Example
sudo yum install gcc       # RHEL/CentOS
# or
sudo apt-get install build-essential  # Debian/Ubuntu
```

**Alternative — cross-compile from a connected machine:** If you cannot install a C compiler on the air-gapped server, build the binary on your connected dev machine and copy only the binary:

```bash
# On your connected dev machine
GOOS=linux GOARCH=amd64 CGO_ENABLED=1 go build -mod=vendor -o mem-sys ./cmd/mem-sys

# Copy the resulting `mem-sys` binary to the server — no build tools needed there
```

## Run

The binary supports two modes:

### HTTP REST server (default)

```bash
./mem-sys --db mem-sys.db --http :8080
```

Flags:
- `--db` — SQLite database file path (default `mem-sys.db`). Use `:memory:` for in-memory.
- `--http` — HTTP listen address (default `localhost:8080`).
- `--mcp` — run as MCP stdio server instead of HTTP.

Example output:
```
time=2026-10-17T06:53:20Z level=INFO msg=listening addr=:8080
```

### MCP stdio server

```bash
MEM_SYS_PRINCIPAL=user-local ./mem-sys --mcp
```

Speaks JSON-RPC 2.0 over stdin/stdout. Use an MCP client (e.g. Claude Desktop, Cursor, or an MCP inspector) to connect.

## Seed Data (auto-seeded on startup)

On first launch the system creates:
| Principal ID | Name | Type | Global Role |
|---|---|---|---|
| `admin-alice` | Alice | human | admin |
| `user-bob` | Bob | human | user |
| `agent-dev` | dev-agent | agent | user |

And one module:
- `auth` ("Authentication module"), owned by `admin-alice`.

## Example Workflow (curl)

Start the server:
```bash
./mem-sys --db /tmp/mem.db --http :8080 &
```

1. **Check identity (Alice, admin + owner)**
```bash
curl -s -H "Authorization: Bearer admin-alice" \
  http://localhost:8080/api/v1/me
```

2. **List modules**
```bash
curl -s -H "Authorization: Bearer admin-alice" \
  http://localhost:8080/api/v1/modules
```

3. **Generate an idempotency key** (optional, prevents duplicate submissions on retry)
```bash
curl -s -H "Authorization: Bearer user-bob" \
  -H "Content-Type: application/json" \
  -d '{"principal_id":"user-bob","max_uses":1,"ttl_seconds":3600}' \
  http://localhost:8080/api/v1/idempotency-keys
```

4. **Bob submits a proposal** (capture the `proposal.id` from the response; pass the `idempotency_key` from step 3)
```bash
curl -s -H "Authorization: Bearer user-bob" \
  -H "Content-Type: application/json" \
  -d '{"text":"The login endpoint rate limits users to 5 attempts per minute.","module_hint":"auth","intent":"new_fact","idempotency_key":"<IDEMPOTENCY_KEY>"}' \
  http://localhost:8080/api/v1/proposals
```

5. **Alice (module owner) accepts the proposal**
```bash
curl -s -H "Authorization: Bearer admin-alice" \
  -H "Content-Type: application/json" \
  -d '{"action":"accept","acting_role":"module_owner","edited_title":"Login rate limit","edited_body":"The login endpoint allows 5 attempts per minute per user."}' \
  http://localhost:8080/api/v1/proposals/<PROPOSAL_ID>/review
```

6. **List facts in the auth module**
```bash
curl -s -H "Authorization: Bearer admin-alice" \
  http://localhost:8080/api/v1/modules/<MODULE_ID>/facts
```

7. **Export module as JSON**
```bash
curl -s -H "Authorization: Bearer admin-alice" \
  -H "Content-Type: application/json" \
  -d '{"module_id":"<MODULE_ID>","format":"json"}' \
  http://localhost:8080/api/v1/exports
```

8. **Export as Markdown**
```bash
curl -s -H "Authorization: Bearer admin-alice" \
  -H "Content-Type: application/json" \
  -d '{"module_id":"<MODULE_ID>","format":"markdown"}' \
  http://localhost:8080/api/v1/exports
```

## Run Tests

```bash
go test ./...
```

Expected:
```
ok  mem-sys/internal/http
ok  mem-sys/internal/service
```

### Test suites

| Test | File | Covers |
|---|---|---|
| `TestMVPFlow` | `internal/service/service_test.go` | Submit → accept → versioned fact → revision → active placement → export → audit events (in-process store + service) |
| `TestSmokeFlow` | `internal/http/smoke_test.go` | Live HTTP smoke test via `httptest`: `/me`, list modules, submit proposal, owner accept, list facts, fact detail, JSON export, 401 without token, 403 for non-owner review |
| `TestIdempotencyKeyFlow` | `internal/service/service_test.go` | Idempotency key lifecycle: generate, consume, max-uses enforcement, expiry, nonexistent key, custom TTL/max_uses, invalid principal |
| `TestIdempotencyKeyHTTP` | `internal/http/smoke_test.go` | HTTP endpoint `POST /api/v1/idempotency-keys`: create with explicit principal, default principal (actor), nonexistent principal → 400, max_uses=0 defaults to 1 |

## Stop

```bash
pkill -f mem-sys
rm -f mem-sys.db   # remove SQLite file to reset state
```

## Notes

- SQLite is single-writer; the store sets `SetMaxOpenConns(1)` for safety in the MVP.
- Database migrations run automatically on startup (`migrations/001_core_mvp.sql`, `migrations/002_idempotency_keys.sql`).
- To reset state, delete the `--db` file and restart.
- No external services (Postgres, vector DB, LLM) are required for the MVP.
