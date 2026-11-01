# mem-sys

A governed, persistent knowledge/memory system for a changing codebase. Proposals are submitted via HTTP REST or MCP, reviewed by module owners, and maintained as versioned facts with explicit module placements.

## MVP (per spec §21)

- Admin creates modules and assigns multiple owners
- One person can own multiple modules and be admin simultaneously
- Users/agents submit proposals (idempotent, with server-generated keys via `POST /idempotency-keys`)
- Module owners accept / edit-and-accept / reject / reassign proposals
- Accepted proposals become versioned facts with active placements
- Facts move between modules via inbound placement (target owner approval)
- JSON / Markdown export per module
- Append-only audit log (role used, admin override, reason)

## Quick Start

```bash
git clone <repo> mem-sys
cd mem-sys
go build -o mem-sys ./cmd/mem-sys
./mem-sys --db mem-sys.db --http :8080
```

### Offline / Air-Gapped Deployment

The project includes vendored dependencies (`vendor/`). To build on a server with no internet:

```bash
go build -mod=vendor -o mem-sys ./cmd/mem-sys
```

A C compiler (`gcc`/`clang`) is required for the CGO-based SQLite driver. Alternatively, cross-compile from a connected machine and copy only the binary:

```bash
GOOS=linux GOARCH=amd64 CGO_ENABLED=1 go build -mod=vendor -o mem-sys ./cmd/mem-sys
```

Seed data is created automatically:
- `admin-alice` (admin, owns `auth` module)
- `user-bob` (user)
- `agent-dev` (agent)

### Example: generate idempotency key → submit → accept a proposal

```bash
# 1. Generate an idempotency key
curl -s -H "Authorization: Bearer user-bob" \
  -H "Content-Type: application/json" \
  -d '{"principal_id":"user-bob","max_uses":1}' \
  http://localhost:8080/api/v1/idempotency-keys

# 2. Bob submits with the key
curl -s -H "Authorization: Bearer user-bob" \
  -H "Content-Type: application/json" \
  -d '{"text":"Login rate limit 5/min","module_hint":"auth","idempotency_key":"<KEY>"}' \
  http://localhost:8080/api/v1/proposals

# 3. Alice (module owner) accepts
curl -s -H "Authorization: Bearer admin-alice" \
  -H "Content-Type: application/json" \
  -d '{"action":"accept","acting_role":"module_owner"}' \
  http://localhost:8080/api/v1/proposals/<ID>/review
```

### MCP mode

```bash
MEM_SYS_PRINCIPAL=user-local ./mem-sys --mcp
```

JSON-RPC 2.0 over stdio. Tools: `propose_fact`, `review_proposal`, `list_modules`, `get_fact`, `move_fact`, `export_module`, `generate_idempotency_key`, etc.

## Tests

```bash
go test ./...
```

## Docs

| File | Contents |
|---|---|
| `docs/spec.md` | Full specification |
| `docs/architecture.md` | Backend architecture |
| `docs/api.md` | REST + MCP API reference |
| `docs/runbook.md` | Prerequisites, build, run, examples |
| `docs/url-prefill-form-plan.md` | Pre-filled proposal form link feature plan |
| `tools/module_pattern_gen.md` | Module path pattern generator utility |

## Stack

Go · SQLite · chi (HTTP) · stdio MCP adapter

## License

Unspecified
