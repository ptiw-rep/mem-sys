# API Documentation

mem-sys exposes two adapters over the same service layer:

1. **HTTP REST** — `http://<host>:<port>/api/v1` (bearer auth)
2. **MCP stdio** — JSON-RPC 2.0 over stdin/stdout (tools interface)

Authentication: `Authorization: Bearer <principal_id>`. In the MVP the bearer token **is** the principal ID (`admin-alice`, `user-bob`, `agent-dev` are seeded).

Base path: `/api/v1`

---

## 1. Identity

### GET /me
Returns the authenticated principal's global role and module ownerships.

**Response**
```json
{
  "principal_id": "admin-alice",
  "global_role": "admin",
  "module_roles": [
    { "module_id": "3b5e...", "role": "owner" }
  ]
}
```

---

## 2. Modules

### GET /modules
List all modules.

### POST /modules
Create a module. **Admin only.**

**Request**
```json
{
  "name": "auth",
  "description": "Authentication module",
  "path_patterns": ["services/auth/**"],
  "exclude_patterns": [],
  "owner_ids": ["admin-alice", "user-bob"]
}
```

### GET /modules/{module_id}
Get a module (includes its owners).

### PATCH /modules/{module_id}
Update module fields (e.g. `{"status": "read_only"}`). Admin only.

### GET /modules/{module_id}/facts
List active facts placed in the module.

### GET /modules/{module_id}/proposals
List proposals targeted at the module.

### POST /modules/{module_id}/owners
Grant module ownership. **Admin only.**
```json
{ "principal_id": "user-bob" }
```

### DELETE /modules/{module_id}/owners/{principal_id}
Revoke module ownership. Admin only.

### POST /modules/{module_id}/decommission
Decommission a module (placeholder in MVP). Admin only.

---

## 3. Proposals

### POST /proposals
Submit a proposal. Idempotency via `Idempotency-Key` header or `idempotency_key` body field.

**Request**
```json
{
  "text": "The login endpoint rate limits users to 5 attempts per minute.",
  "module_hint": "auth",
  "file_hint": "services/auth/login.go",
  "symbol_hint": "handleLogin",
  "intent": "new_fact",
  "idempotency_key": "prop-login-001",
  "mapping_confidence": 0.9
}
```

### GET /proposals?module_id=X&status=Y
List proposals (optional module/status filter).

### GET /proposals/{proposal_id}
Get a single proposal.

### POST /proposals/{proposal_id}/review
Review a proposal. Module owner or admin (override) only.

**Request** (accept)
```json
{
  "action": "accept",
  "acting_role": "module_owner",
  "use_admin_override": false,
  "edited_title": "Login rate limit",
  "edited_body": "The login endpoint allows 5 attempts per minute per user.",
  "comment": "Looks correct."
}
```

**Request** (admin override on non-owned module)
```json
{
  "action": "accept",
  "acting_role": "admin",
  "use_admin_override": true,
  "admin_override_reason": "Module owner unavailable",
  "comment": "Approved by admin override."
}
```

Supported `action` values: `accept`, `accept_with_edits`, `reject`, `duplicate`, `reassign`, `request_clarification`. For `reassign`, include `new_module_id`.

**Response** — the updated proposal.

---

## 4. Idempotency Keys

Idempotency keys are single-use (or limited-use) tokens that prevent duplicate proposal submissions when clients retry. They are generated server-side and then passed to `POST /proposals` (or `propose_fact` MCP tool) via the `idempotency_key` field/header.

### POST /idempotency-keys
Generate a new idempotency key. The key can then be reused across retries until it expires or reaches its max uses.

**Request**
```json
{
  "principal_id": "user-bob",
  "max_uses": 1,
  "ttl_seconds": 3600
}
```

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `principal_id` | string | No | authenticated actor | Principal who owns the key |
| `max_uses` | int | No | `1` | Maximum number of times the key can be consumed |
| `ttl_seconds` | int | No | `86400` (24h) | Time-to-live in seconds before the key expires |

**Response** (201)
```json
{
  "idempotency_key": {
    "key": "prop-login-001",
    "principal_id": "user-bob",
    "expires_at": "2026-10-18T06:53:20Z",
    "max_uses": 1,
    "used_count": 0,
    "created_at": "2026-10-17T06:53:20Z"
  }
}
```

**Validation rules (corner cases):**
- `principal_id` must reference an existing principal, otherwise `400`.
- If `principal_id` is omitted, the authenticated bearer-token principal is used.
- If `max_uses` is `0` or negative, it defaults to `1`.
- If `ttl_seconds` is `0` or negative, it defaults to `86400` (24 hours).
- Once `used_count >= max_uses`, the key can no longer be consumed.
- Once `expires_at` is in the past, the key can no longer be consumed.
- A key that does not exist returns `400` on consume.

Idempotency keys are consumed when a proposal is created with that key and a matching proposal already exists (the existing proposal is returned instead of creating a duplicate).

---

## 5. Facts

### GET /facts
List all active facts.

### GET /facts/{fact_id}
Get a fact with its revisions, placements, and anchors.

### GET /facts/{fact_id}/revisions
List immutable revisions of a fact.

### POST /facts/{fact_id}/deprecate
Deprecate a fact (status → `deprecated`).

### POST /facts/{fact_id}/archive
Archive a fact (status → `archived`).

### POST /facts/{fact_id}/move
Propose moving a fact to another module. Creates a `MoveRequest` (status `proposed`).

**Request**
```json
{
  "to_module_id": "module-billing",
  "reason": "Retry logic now belongs to billing infrastructure."
}
```

---

## 6. Move Requests

### POST /move-requests
Create a move request (same as `POST /facts/{id}/move`).

### GET /move-requests/{move_id}
Get a move request.

### POST /move-requests/{move_id}/review
Review (accept/reject) an inbound move. Target module owner or admin override only.

**Request**
```json
{
  "action": "accept",
  "use_admin_override": false,
  "comment": "Accepted into billing module."
}
```

On accept: target placement becomes `active`, source placement archived, move request → `accepted`.

---

## 7. Exports

### POST /exports
Export module data.

**Request**
```json
{
  "module_id": "module-auth",
  "format": "json"   // json | markdown
}
```

**Response** (json) — `application/json` body with `export_version`, `exported_at`, `scope`, `modules`, `facts`, `proposals`, `events`.

**Response** (markdown) — `text/markdown` body.

### GET /exports/{export_id}
Export status (inline in MVP).

---

## 8. Error Responses

```json
{ "error": "unauthorized" }          // 401
{ "error": "admin required" }        // 403
{ "error": "module not found" }      // 404
{ "error": "bad request message" }   // 400
```

---

## 9. MCP Interface

Transport: stdio JSON-RPC 2.0. Run with `--mcp`.

**Initialize**
```json
{ "jsonrpc": "2.0", "id": 1, "method": "initialize" }
```

**Tools list**
```json
{ "jsonrpc": "2.0", "id": 2, "method": "tools/list" }
```

**Tools call** (example: propose_fact)
```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "method": "tools/call",
  "params": {
    "name": "propose_fact",
    "arguments": {
      "text": "Auth tokens expire after 15 minutes.",
      "module_hint": "auth",
      "idempotency_key": "mcp-001"
    }
  }
}
```

Available tools: `get_my_roles`, `list_modules`, `get_module`, `list_module_facts`, `get_fact`, `get_fact_history`, `propose_fact`, `list_proposals`, `get_proposal`, `review_proposal`, `move_fact`, `list_move_requests`, `review_move_request`, `add_module`, `decommission_module`, `export_module`, `get_anchor_status`, `generate_idempotency_key`.

The MCP principal is set via env `MEM_SYS_PRINCIPAL` (default `user-local`).
