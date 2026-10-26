# Backend Architecture

## Overview

mem-sys is a persistent, governed knowledge/memory system for a changing codebase. The MVP implements **Phase 1: Core Governance** of the spec (docs/spec.md, section 20-21): proposals can be submitted, reviewed, accepted, versioned into facts, moved between modules, exported, and audited.

The backend is implemented in **Go** with **SQLite** for storage, **chi** for HTTP routing, and a **stdio MCP server** adapter.

## Design Principles (from spec)

| Principle | Implementation |
|---|---|
| No silent information loss | All actions append to `event_log`; rejected proposals retained; deletions prefer tombstones |
| Facts independent of code location | `Fact` identity is a UUID; anchors are metadata attached to facts |
| Module placement is governed | `FactPlacement` rows govern which module a fact belongs to; moves require target acceptance |
| Everything versioned and auditable | `fact_revisions` are immutable; every write appends an `event_log` row |
| Human approval for semantic changes | Proposals require module-owner/admin review before becoming facts |
| Roles not mutually exclusive | Principal has a global role (`user\|admin`) plus zero-or-more module-owner grants |

## Layered Architecture

```
┌─────────────────────────────────────────────────────────┐
│  Adapters                                               │
│  ├── HTTP REST  (chi)   /api/v1                         │
│  └── MCP stdio  (JSON-RPC 2.0 over stdin/stdout)        │
└──────────────────────┬──────────────────────────────────┘
                       │
┌──────────────────────▼──────────────────────────────────┐
│  Service Layer  (internal/service)                      │
│  ├── Authentication & Authorization (RBAC)              │
│  ├── Proposals (submit, review, accept, reject, reassign)│
│  ├── Facts & Revisions (versioned, placements)          │
│  ├── Moves (inbound placement, target approval)         │
│  ├── Export (JSON / Markdown)                           │
│  └── Audit (append-only event log)                      │
└──────────────────────┬──────────────────────────────────┘
                       │
┌──────────────────────▼──────────────────────────────────┐
│  Store Layer  (internal/store)                          │
│  ├── principals, modules, module_owners                 │
│  ├── proposals, fact_revisions, fact_placements         │
│  ├── move_requests, facts_anchors, idempotency_keys, event_log            │
│  └── SQLite (via database/sql + go-sqlite3)             │
└─────────────────────────────────────────────────────────┘
```

### Layer responsibilities

**Adapters (HTTP + MCP)** — Parse requests, enforce bearer-token auth, translate to service calls. Business logic lives only in the service layer; both adapters share it.

**Service layer** — Core domain logic:
- `Authenticate` resolves a bearer token (principal ID) into an `Actor` with `global_role` and module-owner grants.
- `CanReviewModule` computes effective access: owner → `module_owner`; admin not owner → `admin_override` (reason required if configured); otherwise deny.
- `CreateProposal`, `ReviewProposal`, `MoveFact`, `ReviewMoveRequest`, `ExportModule`, `GenerateIdempotencyKey`.

**Store layer** — Thin CRUD over SQLite. Each entity has create/get/list helpers. `Store.Tx` wraps multi-statement transactions.

**DB layer** — Opens SQLite, applies migrations from `migrations/001_core_mvp.sql` on startup, enforces `PRAGMA foreign_keys = ON` and a single open connection (SQLite simplicity for MVP).

## Domain Model (MVP entities)

- `Principal` — id, name, type (human\|agent\|service), global_role (user\|admin), created_at
- `Module` — id, name (unique), description, path_patterns, status, created_by, created_at
- `ModuleOwner` — (module_id, principal_id) PK; many-to-many ownership
- `Proposal` — id, idempotency_key, submitter_id, source, raw_payload, normalized_text, intent, target_module_id, status, mapping_confidence, …
- `Fact` — id, title, kind, status, current_revision_id, origin_proposal_id, created_by
- `FactRevision` — immutable; id, fact_id, version, title, body, author_id, approved_by, source_proposal_id, created_at
- `FactPlacement` — id, fact_id, module_id, placement_type, status (active\|inbound_proposed\|archived\|rejected), approved_by
- `MoveRequest` — id, fact_id, from/to module, status, initiator, approvals, role_used, admin_override flag
- `FactAnchor` — id, fact_id, anchor_type, path, symbol, status, confidence, …
- `IdempotencyKey` — id, principal_id, expires_at, max_uses, used_count, created_at (single-use or limited-use tokens to prevent duplicate submissions)
- `Event` — append-only audit: aggregate_type/id, actor_id, role_used, admin_override_used, action, payload, occurred_at

## Role & Access Model

A `Principal` has:
- One global role: `user` or `admin`
- Zero or more `ModuleOwner` grants (module-scoped)

Effective access for a module-scoped action:
1. If principal is module owner → `module_owner` (preferred, even if also admin)
2. Else if principal is admin → `admin_override` (reason required)
3. Else → deny

Every mutation records `actor_id`, `actor_global_role`, `role_used`, `module_id`, `admin_override_used`, `reason`, `timestamp` in `event_log`.

## State Machines (MVP subset)

- Proposal: `submitted → ready_for_review → accepted \| accepted_with_edits \| rejected \| duplicate \| needs_clarification`
- Fact: `active → needs_review \| disputed \| deprecated \| archived`
- FactPlacement: `inbound_proposed → active \| archived \| rejected`
- MoveRequest: `proposed → accepted \| rejected`
- Module: `active → read_only \| decommissioning → archived`

## Data Flow Examples

### Submit → Accept a proposal
1. Client POSTs `/api/v1/proposals` with text + module_hint.
2. Service creates `Proposal` (status `submitted`), appends `proposal.created` event.
3. Module owner POSTs `/api/v1/proposals/{id}/review` with `action: accept`.
4. Service creates `Fact`, `FactRevision` v1, active `FactPlacement`; flips proposal to `accepted`; appends `proposal.reviewed` + `fact.created` events.

### Move a fact between modules
1. Client POSTs `/api/v1/facts/{id}/move` with `to_module_id`.
2. Service creates `MoveRequest` (status `proposed`) after source-side authz.
3. Target module owner POSTs `/api/v1/move-requests/{id}/review` with `action: accept`.
4. Service activates target placement, archives source placement, flips move to `accepted`.

## Project Layout

```
mem-sys/
├── cmd/mem-sys/main.go          # entrypoint (HTTP or --mcp stdio)
├── docs/                        # spec.md + architecture/api/runbook docs
├── migrations/001_core_mvp.sql  # core MVP SQLite schema
├── migrations/002_idempotency_keys.sql  # idempotency key registry schema
├── go.mod / go.sum
└── internal/
    ├── db/                      # SQLite connection + migrations
    ├── store/                   # entity CRUD (principals, modules, proposals, facts, moves, anchors, events)
    ├── service/                 # domain logic + authz + export
    ├── http/                    # chi REST server + middleware + handlers
    └── mcp/                     # stdio JSON-RPC 2.0 server (tools list/call)
```

## Security Notes (MVP)

- Authentication: bearer token where token = principal ID (simple for MVP; OIDC later).
- Authorization enforced in the service layer, not only the handler layer.
- Admin override requires a reason (configurable).
- Hard delete is not exposed in MVP; deprecation/archive preferred.
- Idempotency keys supported on proposal creation (`POST /idempotency-keys`, `generate_idempotency_key` MCP tool).

## What's Deferred (post-MVP)

Full AST indexing, semantic embeddings, anchor auto-repair, conflict detection, full web UI, repo sync/webhooks, OIDC, rate limiting.
