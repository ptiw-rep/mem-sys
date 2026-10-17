# mem-sys

A governed, persistent knowledge/memory system for a changing codebase. Proposals are submitted via HTTP REST or MCP, reviewed by module owners, and maintained as versioned facts with explicit module placements.

## MVP (per spec §21)

- Admin creates modules and assigns multiple owners
- One person can own multiple modules and be admin simultaneously
- Users/agents submit proposals (idempotent)
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

Seed data is created automatically:
- `admin-alice` (admin, owns `auth` module)
- `user-bob` (user)
- `agent-dev` (agent)

### Example: submit → accept a proposal

```bash
# Bob submits
curl -s -H "Authorization: Bearer user-bob" \
  -H "Content-Type: application/json" \
  -d '{"text":"Login rate limit 5/min","module_hint":"auth","idempotency_key":"p1"}' \
  http://localhost:8080/api/v1/proposals

# Alice (module owner) accepts
curl -s -H "Authorization: Bearer admin-alice" \
  -H "Content-Type: application/json" \
  -d '{"action":"accept","acting_role":"module_owner"}' \
  http://localhost:8080/api/v1/proposals/<ID>/review
```

### MCP mode

```bash
MEM_SYS_PRINCIPAL=user-local ./mem-sys --mcp
```

JSON-RPC 2.0 over stdio. Tools: `propose_fact`, `review_proposal`, `list_modules`, `get_fact`, `move_fact`, `export_module`, etc.

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

## Stack

Go · SQLite · chi (HTTP) · stdio MCP adapter

## License

Unspecified
