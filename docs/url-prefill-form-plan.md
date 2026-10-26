# Feature Plan: Pre-filled Proposal Form Links (URL Input Channel) for mem-sys

**Status:** Proposed (additive extension)  
**Scope:** New way to submit proposals into mem-sys via pre-filled form URLs  
**Schema impact:** None for the form itself. A companion additive migration `migrations/002_idempotency_keys.sql` adds the `idempotency_keys` table to support idempotency key generation via `POST /api/v1/idempotency-keys` or the `generate_idempotency_key` MCP tool.  
**Existing API impact:** None (all current insert/update/delete paths unchanged)

---

## 1. Goal

Let anyone (human or agent) open a single link that opens a proposal form with context pre-filled (module, file, symbol, text, intent, etc.), then submit it into mem-sys through the **existing** proposal ingestion pipeline.

This mirrors quick-back's `/?action_item_id=...&file_path=...&comment_text=...` pattern, applied to mem-sys proposals.

---

## 2. Non-Goals (explicitly out of scope)

- ❌ No changes to `migrations/001_core_mvp.sql`
- ❌ No changes to `internal/store/*` entity structs
- ❌ No changes to `internal/service/*` existing methods
- ❌ No changes to existing `POST /proposals`, `GET /proposals`, etc.
- ❌ No auth changes (the form submits with whatever bearer token the caller provides; unauthenticated submissions resolve to the default/anonymous principal, matching today's behavior)
- ❌ No full web UI (this is one lightweight form page + link generator)

---

## 3. Design

### 3.1 Two additive endpoints only

| Endpoint | Method | Purpose |
|---|---|---|
| `GET /api/v1/proposals/link` | Generate a shareable pre-filled proposal URL from query params |
| `GET /form` | Serve a minimal HTML form that reads query params and POSTs to `POST /api/v1/proposals` |

Both are **new** routes; nothing existing is touched.

### 3.2 URL format (query params)

A pre-filled proposal link looks like:

```
/form?module_hint=auth&file_hint=services/auth/login.go&symbol_hint=handleLogin&text=Login+rate+limit+is+5+per+minute.&intent=new_fact&idempotency_key=prop-001
```

Supported query params (all optional except `text` recommended):

| Param | Maps to | Notes |
|---|---|---|
| `text` | `normalized_text` / `raw_payload` | The fact text |
| `module_hint` | `target_module_id` (resolved by name) | Module name, e.g. `auth` |
| `file_hint` | raw payload context | File path hint |
| `symbol_hint` | raw payload context | Symbol/function hint |
| `line_hint` | raw payload context | Line number(s) |
| `repo_hint` | raw payload context | Repo name |
| `intent` | `intent` | `new_fact`, `amend_fact`, etc. Defaults to `new_fact` |
| `idempotency_key` | `idempotency_key` | Dedupe on retry |
| `mapping_confidence` | `mapping_confidence` | Optional float |

### 3.3 Flow

1. **Generate an idempotency key** (optional, prevents duplicate submissions): caller hits `POST /api/v1/idempotency-keys` (HTTP) or `generate_idempotency_key` (MCP) → receives a key string with TTL and max-uses.
2. **Generate link** (optional convenience): caller hits `GET /api/v1/proposals/link?module_hint=auth&text=...&idempotency_key=<KEY>` → receives a JSON object with the full `/form?...` URL.
3. **Open link:** user/agent opens `/form?module_hint=auth&text=...` in a browser or agent constructs the HTTP request directly.
4. **Form renders:** context fields (module, file, symbol, text) are shown as read-only pre-filled values.
5. **Submit:** user confirms/edits and submits → form POSTs to `POST /api/v1/proposals` with the bearer token in the `Authorization` header and the `idempotency_key`.
6. **Existing pipeline runs:** `CreateProposal` → immutable raw proposal → `submitted` status → ready for owner review → accepted → versioned fact. Exactly as today.

### 3.4 Example generated link

```
GET /api/v1/proposals/link?module_hint=auth&file_hint=services/auth/login.go&symbol_hint=handleLogin&text=The+login+endpoint+rate+limits+users+to+5+attempts+per+minute.&intent=new_fact&idempotency_key=prop-login-001
```

Response:
```json
{
  "url": "http://localhost:8080/form?module_hint=auth&file_hint=services%2Fauth%2Flogin.go&symbol_hint=handleLogin&text=The+login+endpoint+rate+limits+users+to+5+attempts+per+minute.&intent=new_fact&idempotency_key=prop-login-001",
  "query_params": {
    "module_hint": "auth",
    "file_hint": "services/auth/login.go",
    "symbol_hint": "handleLogin",
    "text": "The login endpoint rate limits users to 5 attempts per minute.",
    "intent": "new_fact",
    "idempotency_key": "prop-login-001"
  }
}
```

An agent or script can also construct `/form?...` directly without calling the link endpoint.

---

## 4. Implementation Plan (additive only)

### Step 1: Add `GET /api/v1/proposals/link` handler

New file: `internal/http/prefill.go` (or add to `handlers.go`).

```go
func (s *Server) handleGenerateProposalLink(w, r) {
    // Read query params
    // Build /form?... URL with url.QueryEscape
    // Return JSON { url, query_params }
}
```

Route addition in `internal/http/server.go`:
```go
r.Get("/proposals/link", s.handleGenerateProposalLink)
```

### Step 2: Add `GET /form` handler (minimal HTML form)

Same file or `internal/http/form.go`. Returns a small HTML page with inline JS:

- Reads `window.location.search` via `URLSearchParams`
- Displays read-only context fields (module, file, symbol, text, intent)
- Editable "confirm/edit" text area (defaults to `text`)
- On submit: `fetch("POST /api/v1/proposals", { headers: { Authorization: ... }, body: JSON })`
- Shows success/error message

Route:
```go
r.Get("/form", s.handleFormPage)
```

No template engine needed — inline HTML string in Go.

### Step 3: Wire routes in `server.go`

Add the two new `r.Get(...)` lines inside the `/api/v1` group (for `/proposals/link`) and root group (for `/form`). No existing routes modified.

### Step 4: Tests

Add `TestPreFillLink` and `TestFormPage` in `internal/http/smoke_test.go` (or a new `prefill_test.go`) using `httptest.NewRecorder`:
- Link endpoint returns 200 with valid URL containing escaped params
- Form page returns 200 HTML containing the pre-filled params
- Submitting form params to `POST /proposals` creates a proposal (end-to-end via existing handler)

---

## 5. Schema & Data Impact

**None.** The form submits to the existing `POST /proposals` handler, which calls the existing `service.CreateProposal` → `store.CreateProposal`. No new tables, columns, or migrations.

---

## 6. Security Considerations

- The form is a client-side page; it does not authenticate. The bearer token must be supplied by the page/agent (e.g. from `localStorage`, env, or the agent's token store).
- `idempotency_key` prevents duplicate proposals on retry/refresh.
- Since this writes to the same `POST /proposals`, all existing RBAC, validation, and audit logging (`proposal.created` event) apply unchanged.

---

## 7. Example Usage (curl)

```bash
# 1. Generate an idempotency key (prevents duplicates on retry)
curl -s -H "Authorization: Bearer user-bob" \
  -H "Content-Type: application/json" \
  -d '{"principal_id":"user-bob","max_uses":1,"ttl_seconds":3600}' \
  http://localhost:8080/api/v1/idempotency-keys
# => {"idempotency_key":{"key":"prop-login-001",...}}

# 2. Generate a pre-filled link using that key
curl -s "http://localhost:8080/api/v1/proposals/link?module_hint=auth&text=Login+rate+limit+5/min&intent=new_fact&idempotency_key=prop-login-001"

# 3. Open the returned /form?... URL in a browser, or have an agent POST directly:
curl -s -X POST "http://localhost:8080/api/v1/proposals" \
  -H "Authorization: Bearer user-bob" \
  -H "Content-Type: application/json" \
  -d '{"text":"Login rate limit 5/min","module_hint":"auth","intent":"new_fact","idempotency_key":"prop-login-001"}'
```

Both produce the same proposal record; the form link is just a friendlier way to construct that request.

---

## 8. Deliverables

1. `migrations/002_idempotency_keys.sql` — `idempotency_keys` table schema
2. `internal/store/idempotency_keys.go` — `CreateIdempotencyKey`, `GetIdempotencyKey`, `ValidateAndConsumeIdempotencyKey`
3. `internal/service/service.go` — `GenerateIdempotencyKey` service method + `Config.DefaultIdempotencyTTL` / `Config.DefaultIdempotencyMaxUses`
4. `internal/http/handlers.go` — `handleCreateIdempotencyKey` (`POST /api/v1/idempotency-keys`)
5. `internal/http/server.go` — route registration (additive `r.Post`)
6. `internal/mcp/server.go` — `generate_idempotency_key` tool
7. `internal/http/prefill.go` — `handleGenerateProposalLink` + `handleFormPage`
8. Routes registered in `internal/http/server.go` (two additive `r.Get` lines)
9. Tests: `TestIdempotencyKeyFlow`, `TestIdempotencyKeyHTTP`, `TestPreFillLink`, `TestFormPage`
10. Updated `docs/runbook.md` and `docs/api.md`
11. No changes to existing insert/update/delete paths, existing handlers, or `migrations/001_core_mvp.sql`
