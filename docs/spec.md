 
## Custom AI Persistent Memory System for a Changing Codebase

**Version:** 1.1  
**Status:** Final updated plan for implementation  
**Scope:** MCP server + HTTP REST endpoints + future UI support  
**Preferred implementation languages:** Go and/or Python

---

# 1. Purpose

This system is a persistent, governed knowledge/memory system for a codebase where multiple humans and AI agents submit information about files, functions, modules, behaviors, constraints, and design decisions.

The system must:

1. Accept proposals from users and agents through:
   - MCP
   - HTTP REST endpoints
   - UI

2. Map each proposal to relevant code locations:
   - module
   - file
   - function
   - line range
   - symbol
   - semantic context

3. Route proposals to module owners for approval.

4. Allow module owners to:
   - accept
   - edit and accept
   - reject
   - reassign
   - request clarification

5. Maintain accepted facts per module.

6. Support later edits, removals, moves, splits, and merges of accepted facts.

7. Survive codebase changes:
   - file moves
   - function renames
   - line number shifts
   - functionality moving between modules

8. Support module lifecycle:
   - adding modules
   - removing modules
   - reassigning facts when modules are removed

9. Provide full visibility into:
   - Proposal Step
   - Acceptance Step
   - accepted facts
   - historical facts
   - rejected facts
   - deleted/tombstoned facts
   - exports

10. Ensure information is not silently lost.

---

# 2. Updated Functional Requirements

The final system must support:

## 2.1 Access modes

There are three logical access levels:

1. **General user / agent**
   - can submit proposals
   - can view allowed information
   - can comment or clarify if permitted

2. **Module owner**
   - can review proposals for owned modules
   - can accept, edit, reject, or reassign proposals
   - can manage accepted facts inside owned modules
   - can initiate or approve moves depending on policy

3. **Admin**
   - can manage modules
   - can assign owners
   - can override decisions
   - can manage global settings
   - can perform destructive operations when allowed

## 2.2 New ownership requirements

These must now be supported:

1. **One person can be module owner of multiple modules.**

2. **One person can be both admin and module owner.**

Therefore, roles must not be modeled as a single mutually exclusive role.

Instead:

```text
Principal
  has global role: user | admin
  may have zero or more module owner grants
```

---

# 3. Core Design Principles

These principles are non-negotiable.

---

## 3.1 No silent information loss

Information may only leave active use through explicit, audited actions:

- rejected
- deprecated
- archived
- moved
- deleted

Even then:

- rejected proposals should remain in the audit/archive layer by default
- deleted facts should preferably become tombstones unless admin explicitly hard deletes
- every action should record who, when, why, and under which role

---

## 3.2 Facts are independent of code location

A fact may remain true even if:

- file path changes
- function name changes
- line numbers shift
- code moves to another directory
- module ownership changes

Therefore:

```text
Fact identity != file path
Fact identity != line number
Fact identity != module
```

Code anchors are metadata attached to facts, not the fact itself.

---

## 3.3 Module placement is governed

A fact should not be silently injected into a module’s accepted list.

This is especially important for moves:

> If a fact is moved from Module A to Module B, it must appear in Module B as an inbound entry/proposal and must be accepted by Module B’s owner.

---

## 3.4 Everything is versioned and auditable

The system should preserve:

- raw proposals
- mapping decisions
- review decisions
- fact revisions
- anchor changes
- placement changes
- module lifecycle events
- exports
- admin overrides

---

## 3.5 Human approval is required for semantic changes

Automation may help with:

- mapping
- anchor repair
- duplicate detection
- module suggestions
- code drift detection

But humans should approve:

- accepted fact content
- fact rejection
- fact movement
- module decommissioning
- destructive operations

---

# 4. Recommended Architecture

```text
+------------------+     +------------------+     +------------------+
| MCP Client       |     | HTTP REST Client |     | Web UI           |
+--------+---------+     +--------+---------+     +--------+---------+
         |                          |                          |
         +-------------+------------+--------------------------+
                       |
              +--------v---------+
              | API / MCP Layer  |
              | AuthN / AuthZ    |
              | RBAC             |
              | Rate limiting    |
              | Idempotency      |
              +--------+---------+
                       |
        +--------------+--------------+
        |                             |
+-------v--------+           +--------v---------+
| Core Domain    |           | Intelligence     |
| Services       |           | Workers          |
|                |           |                  |
| Proposals      |           | Repo sync        |
| Reviews        |           | File index       |
| Facts          |           | Symbol index     |
| Modules        |           | Anchor repair    |
| Placements     |           | Semantic search  |
| Moves          |           | Duplicate detect |
| Exports        |           | Suggestions      |
| Audit          |           |                  |
+-------+--------+           +--------+---------+
        |                             |
        +--------------+--------------+
                       |
              +--------v---------+
              | Storage          |
              |                  |
              | Postgres/SQLite  |
              | Event log        |
              | Vector index     |
              | Export artifacts |
              +------------------+
```

MCP and REST must be adapters over the same core service layer.

Do not implement business logic separately inside MCP tools and REST endpoints.

---

# 5. Recommended Technology Stack

## 5.1 Recommended production architecture

### Core server: Go

Use Go for:

- HTTP API
- MCP server
- RBAC
- workflow/state machines
- persistence
- exports
- audit logging

Reasons:

- strong concurrency
- simple deployment
- good for long-running services
- operationally stable

Suggested Go libraries:

- HTTP: `chi`, `echo`, or standard library
- DB: PostgreSQL, SQLite for lightweight mode
- SQL: `sqlc`, `pgx`, `sqlx`
- Logging: `slog`
- Config: environment variables + YAML
- Auth: bearer tokens first, OIDC later

### Optional intelligence worker: Python

Use Python for:

- tree-sitter parsing
- code indexing
- embeddings
- semantic search
- LLM-assisted proposal mapping
- duplicate/conflict detection

Reasons:

- better ecosystem for code intelligence
- easier LLM/embedding integration
- faster iteration for fuzzy analysis

This gives a strong balance:

> Go owns truth, workflow, consistency, and audit.  
> Python owns code intelligence and AI-assisted suggestions.

---

## 5.2 Simpler alternative: Python-only

If you want a faster prototype:

- FastAPI
- SQLAlchemy / SQLModel
- MCP Python SDK
- tree-sitter
- pgvector or SQLite vector extension
- sentence-transformers or external embeddings

This is simpler initially but requires more discipline around long-term maintainability and performance.

---

# 6. Role and Access Model

---

## 6.1 Do not use a single role field

Bad model:

```text
User
- role: user | owner | admin
```

This fails because:

- one person can own multiple modules
- one person can be admin and owner simultaneously
- module ownership is module-scoped
- admin is global-scoped

---

## 6.2 Recommended identity model

### Principal

```text
Principal
- id
- name
- type: human | agent | service
- global_role: user | admin
- created_at
```

### ModuleOwner

```text
ModuleOwner
- module_id
- principal_id
- granted_by
- granted_at
```

Primary key:

```text
(module_id, principal_id)
```

This supports:

- multiple owners per module
- one person owning multiple modules
- admin also being a module owner
- ownership changes without modifying the principal record

---

## 6.3 Example

```text
Alice:
  global_role: admin
  module_owner_of:
    - auth
    - billing

Bob:
  global_role: user
  module_owner_of:
    - payments

Charlie:
  global_role: user
  module_owner_of: []
```

Alice is both admin and owner of multiple modules.

---

## 6.4 Effective permission model

For any action, compute effective access as:

```text
EffectiveAccess =
  global admin permissions
  + module owner permissions for owned modules
  + general user permissions
```

For module-scoped actions:

```text
if principal.global_role == admin and admin_override_allowed:
    allow as admin override

else if principal is owner of the target module:
    allow as module owner

else:
    deny owner-level action
```

---

## 6.5 Admin who is also owner

Important rule:

> If an admin is also an owner of the module they are acting on, the system should preferably record the action as performed by module owner, not admin override.

If they act on a module they do not own, then it should be recorded as:

```text
admin_override
```

Example:

```text
Alice is admin.
Alice owns module auth.
Alice does not own module payments.
```

If Alice approves a proposal in `auth`:

```text
role_used: module_owner
admin_override: false
```

If Alice approves a proposal in `payments`:

```text
role_used: admin
admin_override: true
reason: required
```

This gives cleaner audit history.

---

## 6.6 Final permission matrix

| Action | General User / Agent | Module Owner of Module A | Admin but not owner | Admin and owner of Module A |
|---|---:|---:|---:|---:|
| View modules/facts | Yes | Yes | Yes | Yes |
| Create proposal | Yes | Yes | Yes | Yes |
| Withdraw own proposal | Yes, before final decision | Yes | Yes | Yes |
| Review Module A proposal | No | Yes | Yes, with admin override | Yes, preferably as owner |
| Edit accepted fact in Module A | No, can propose edit | Yes | Yes, with admin override | Yes, preferably as owner |
| Deprecate fact in Module A | No | Yes | Yes, with admin override | Yes, preferably as owner |
| Move fact out of Module A | Maybe propose only | Yes | Yes, with admin override | Yes, preferably as owner |
| Approve inbound move into Module A | No | Yes | Yes, with admin override | Yes, preferably as owner |
| Create module | No | No | Yes | Yes |
| Archive/remove module | No | No | Yes | Yes |
| Assign module owners | No | No | Yes | Yes |
| Export global data | Scoped | Scoped to owned modules | Yes | Yes |
| Hard delete fact | No | Usually no | Yes | Yes, as admin |

---

## 6.7 Role usage must be recorded

Every review, approval, move, deletion, or override should record:

```text
actor_id
actor_global_role
role_used
module_id
admin_override_used
reason
timestamp
```

Example:

```json
{
  "actor_id": "user-alice",
  "global_role": "admin",
  "role_used": "module_owner",
  "module_id": "module-auth",
  "admin_override": false,
  "action": "proposal.accepted"
}
```

Admin override example:

```json
{
  "actor_id": "user-alice",
  "global_role": "admin",
  "role_used": "admin",
  "module_id": "module-payments",
  "admin_override": true,
  "reason": "Module owner unavailable"
}
```

---

# 7. Core Domain Model

This is the final recommended entity model.

---

## 7.1 Principal

```text
Principal
- id
- name
- type: human | agent | service
- global_role: user | admin
- created_at
```

---

## 7.2 Repository

```text
Repo
- id
- name
- clone_url
- default_branch
- last_synced_commit
- status
```

Even if we support only have one repo, keep this abstraction.

---

## 7.3 Module

```text
Module
- id
- name
- description
- repo_id
- path_patterns
- exclude_patterns
- parent_module_id: nullable
- priority
- status: active | read_only | decommissioning | archived
- approval_policy
- successor_module_id: nullable
- created_by
- created_at
```

Example path patterns:

```json
[
  "services/auth/**",
  "packages/auth/**"
]
```

---

## 7.4 ModuleOwner

```text
ModuleOwner
- module_id
- principal_id
- granted_by
- granted_at
```

Supports:

- multiple owners per module
- one person owning multiple modules
- admin also being an owner

---

## 7.5 Proposal

```text
Proposal
- id
- idempotency_key
- submitter_id
- source: mcp | http | ui | agent | system
- repo_id
- raw_payload
- normalized_text
- intent:
    new_fact |
    amend_fact |
    anchor_update |
    move_fact |
    clarification |
    question
- linked_fact_id: nullable
- target_module_id: nullable
- status
- mapping_confidence
- mapping_explanation
- created_at
- updated_at
```

---

## 7.6 Proposal Mapping Candidates

```text
ProposalMappingCandidate
- proposal_id
- candidate_module_id
- candidate_anchor_id
- confidence
- reason
- selected_by_user: nullable
```

---

## 7.7 Review Decision

```text
ReviewDecision
- id
- proposal_id
- reviewer_id
- reviewer_global_role
- role_used: module_owner | admin_override
- admin_override: boolean
- round
- action:
    accept |
    accept_with_edits |
    reject |
    duplicate |
    reassign |
    defer |
    request_clarification
- payload
- comment
- created_at
```

---

## 7.8 Fact

```text
Fact
- id
- title
- kind:
    behavior |
    constraint |
    architecture |
    decision |
    interface |
    bug |
    todo |
    other
- status:
    active |
    needs_review |
    disputed |
    deprecated |
    archived |
    deleted
- current_revision_id
- origin_proposal_id
- created_by
- created_at
- updated_at
```

---

## 7.9 Fact Revision

```text
FactRevision
- id
- fact_id
- version
- title
- body
- structured_payload
- author_id
- approved_by
- source_proposal_id
- change_reason
- content_hash
- created_at
```

Fact revisions are immutable.

Editing a fact creates a new revision.

---

## 7.10 Fact Placement

This is critical for module ownership and moves.

```text
FactPlacement
- id
- fact_id
- module_id
- placement_type: primary | reference | unassigned
- status:
    active |
    inbound_proposed |
    archived |
    rejected
- proposed_by
- approved_by
- reason
- valid_from
- valid_to
```

A fact is not directly “owned” by a module through destructive foreign key only.

Instead, placement is governed.

---

## 7.11 Fact Anchor

```text
FactAnchor
- id
- fact_id
- revision_id: nullable
- repo_id
- anchor_type:
    path |
    file_blob |
    symbol |
    range |
    content_snippet |
    ast_query |
    semantic_embedding
- path
- symbol_name
- qualified_symbol_name
- symbol_signature_hash
- start_line
- end_line
- context_hash
- tree_sitter_query
- embedding_ref
- confidence
- status:
    valid |
    drifted |
    ambiguous |
    broken |
    unresolved
- last_verified_commit
- created_at
- updated_at
```

Line numbers are hints, not primary identity.

---

## 7.12 Anchor Resolution Event

```text
AnchorResolutionEvent
- id
- anchor_id
- commit_sha
- previous_status
- new_status
- candidate_anchors
- chosen_anchor
- auto_resolved
- confidence
- reason
- created_at
```

---

## 7.13 Move Request

```text
MoveRequest
- id
- fact_id
- from_module_id
- to_module_id
- status:
    proposed |
    pending_source |
    pending_target |
    accepted |
    rejected |
    cancelled
- reason
- initiated_by
- source_approved_by
- target_approved_by
- role_used_source
- role_used_target
- admin_override_used
- created_at
- resolved_at
```

---

## 7.14 Module Decommission Job

```text
ModuleDecommissionJob
- id
- module_id
- initiated_by
- successor_module_id: nullable
- status:
    started |
    reassigning |
    blocked |
    completed
- created_at
- completed_at
```

---

## 7.15 Export Job

```text
ExportJob
- id
- requester_id
- scope
- filters
- format: json | jsonl | markdown | text
- status
- artifact_path
- checksum
- created_at
```

---

## 7.16 Event Log

Append-only audit log.

```text
Event
- id
- aggregate_type
- aggregate_id
- actor_id
- actor_global_role
- role_used
- admin_override_used
- action
- payload
- correlation_id
- occurred_at
```

Examples:

```text
proposal.created
proposal.mapped
proposal.reviewed
fact.created
fact.revision_added
fact.moved
anchor.repaired
module.created
module.decommissioned
admin.override.used
export.completed
```

---

# 8. State Machines

Explicit state machines are strongly recommended.

---

## 8.1 Proposal states

```text
submitted
mapping
needs_clarification
ready_for_review
under_review
accepted
accepted_with_edits
rejected
duplicate
withdrawn
expired
```

---

## 8.2 Fact states

```text
active
needs_review
disputed
deprecated
archived
deleted
```

---

## 8.3 Fact placement states

```text
inbound_proposed
active
archived
rejected
```

---

## 8.4 Move request states

```text
proposed
pending_source
pending_target
accepted
rejected
cancelled
```

---

## 8.5 Module states

```text
active
read_only
decommissioning
archived
```

---

## 8.6 Anchor states

```text
valid
drifted
ambiguous
broken
unresolved
```

---

# 9. End-to-End Workflows

---

# 9.1 Module Creation Workflow

Admin creates a module.

Example:

```json
{
  "name": "auth",
  "description": "Authentication and session handling",
  "repo_id": "repo-main",
  "path_patterns": [
    "services/auth/**",
    "packages/auth/**"
  ],
  "exclude_patterns": [],
  "owner_ids": [
    "user-alice",
    "user-bob"
  ],
  "approval_policy": {
    "new_fact": "any_owner",
    "edit_fact": "any_owner",
    "move_in": "any_owner",
    "move_out": "any_owner"
  }
}
```

Validation rules:

1. Module name should be unique.
2. Path patterns should not be empty unless module is virtual, e.g. `Unassigned`.
3. Overlapping paths should be checked.
4. Most specific path pattern should win.
5. Owners can be zero or more, but ownerless modules should be flagged.
6. Admin can assign multiple owners.
7. One owner can be assigned to many modules.

---

# 9.2 Proposal Step

A human or agent submits information.

Example:

```json
{
  "text": "The login endpoint rate limits users to 5 attempts per minute.",
  "repo_hint": "backend",
  "module_hint": "auth",
  "file_hint": "services/auth/login.go",
  "symbol_hint": "handleLogin",
  "line_hint": [88, 104],
  "metadata": {
    "agent_name": "code-review-agent"
  }
}
```

---

## 9.2.1 Proposal ingestion pipeline

### Step 1: Authenticate

Validate:

- principal identity
- permission to submit proposal
- payload size
- idempotency key

### Step 2: Persist raw proposal

Store original payload immutably.

Status:

```text
submitted
```

### Step 3: Parse hints

Extract:

- module hint
- file hint
- symbol hint
- line hint
- fact type
- linked fact id

### Step 4: Search repository index

Use(optional):

- exact path lookup
- fuzzy path lookup
- symbol index
- AST index
- content hash search
- embedding search
- git rename history

### Step 5: Generate anchor candidates

Each candidate has:

- path
- symbol
- module
- confidence
- reason

### Step 6: Map to module

Use module path patterns.

Rules:

1. Exact path match is strongest.
2. Most specific module path wins.
3. If ambiguous, mark proposal as ambiguous.
4. Do not discard ambiguous proposals.
5. Route ambiguous proposals to triage or admin/owner review.

### Step 7: Set proposal status

Possible outcomes:

```text
ready_for_review
needs_clarification
ready_for_review but ambiguous
```

---

## 9.2.2 Mapping should be explainable

For every proposal, store:

```json
{
  "mapping": {
    "selected_module_id": "module-auth",
    "confidence": 0.91,
    "reasons": [
      "Exact path prefix match",
      "Symbol handleLogin found in services/auth/login.go"
    ],
    "candidates": [
      {
        "module_id": "module-auth",
        "path": "services/auth/login.go",
        "symbol": "handleLogin",
        "confidence": 0.91
      },
      {
        "module_id": "module-api-gateway",
        "path": "gateway/auth.go",
        "symbol": "login",
        "confidence": 0.34
      }
    ]
  }
}
```

Owners must be able to override mapping.

---

# 9.3 Acceptance Step

Module owners review proposals.

The review screen/API should show:

- raw proposal
- submitter
- source
- target module
- mapped file/function/line
- anchor confidence
- duplicate warnings
- related facts
- code snippet if available
- commit at mapping time
- reviewer’s effective role

---

## 9.3.1 Owner actions

### Accept as-is

Creates:

- Fact
- FactRevision v1
- active FactPlacement in target module
- anchors

Proposal status:

```text
accepted
```

---

### Edit and accept

Owner edits:

- title
- body
- anchors
- module
- tags

System preserves:

- original proposal
- edited revision
- editor identity
- role used
- reason

Proposal status:

```text
accepted_with_edits
```

---

### Reject

Requires reason.

Proposal status:

```text
rejected
```

No active fact is created.

Raw proposal remains in archive by default.

---

### Mark duplicate

Link proposal to existing fact.

Proposal status:

```text
duplicate
```

Optional merge action can be taken.

---

### Reassign module

If proposal belongs elsewhere:

- update target module
- preserve history
- notify new module owners
- proposal enters new module review queue

---

### Request clarification

Proposal status:

```text
needs_clarification
```

Submitter can respond.

---

## 9.3.2 Approval policy for multiple owners

Module approval policy options:

```text
any_owner
all_owners
quorum
```

Recommended default:

```text
any_owner
```

Important:

> If a person is admin and owner, their admin status does not count as extra owner votes.  
> One person counts as one owner per module.

---

## 9.3.3 Admin override in acceptance

If admin reviews a module they do not own:

```text
role_used: admin_override
reason: mandatory
```

If admin reviews a module they own:

```text
role_used: module_owner
admin_override: false
```

---

# 9.4 Accepted Fact Management

Once accepted, facts can be:

- viewed
- edited
- deprecated
- archived
- moved
- split
- merged
- exported

---

## 9.4.1 Editing facts

### General user suggests edit

Creates:

```text
Proposal
intent = amend_fact
linked_fact_id = fact-id
```

Goes through normal review.

### Module owner edits directly

Allowed, but creates:

- new FactRevision
- audit event
- role used
- reason

Old revision remains immutable.

---

## 9.4.2 Removing facts

Prefer:

```text
deprecated
```

or:

```text
archived
```

Hard delete should be admin-only.

Even hard delete should leave a tombstone:

```json
{
  "fact_id": "fact-123",
  "deleted_by": "user-alice",
  "role_used": "admin",
  "reason": "Contains sensitive data",
  "hard_delete": false
}
```

---

## 9.4.3 Splitting facts

Used when one fact becomes multiple facts due to code evolution.

Operation:

1. Archive original fact.
2. Create successor facts.
3. Link original to successors.
4. Preserve provenance.

Example:

```text
original_fact_id: fact-123
split_into:
  - fact-456
  - fact-789
```

---

## 9.4.4 Merging facts

Used when duplicate facts exist.

Operation:

1. Choose canonical fact.
2. Archive duplicate fact.
3. Link duplicate to canonical fact.
4. Preserve revisions and history.

---

# 9.5 Moving Facts Between Modules

This is a critical workflow.

Rule:

> A fact moving into a module must appear as an inbound entry/proposal in that module and must be accepted by the target module owner.

---

## 9.5.1 Move workflow

### Step 1: Move proposed

Example:

```json
{
  "fact_id": "fact-123",
  "target_module_id": "module-billing",
  "reason": "Retry logic now belongs to billing infrastructure."
}
```

Create:

```text
MoveRequest
status = proposed
```

---

### Step 2: Source approval

Depending on policy:

- source owner approves
- admin can override
- source approval may be automatic for admin-initiated moves

Record:

```text
source_approved_by
role_used_source
admin_override_used
```

---

### Step 3: Target receives inbound placement

Create:

```text
FactPlacement
- fact_id
- module_id = target module
- status = inbound_proposed
```

This appears in target module’s acceptance queue.

---

### Step 4: Target owner decision

Target owner can:

#### Accept

Then:

- target placement becomes `active`
- source placement becomes `archived`
- move request becomes `accepted`

#### Reject

Then:

- target placement becomes `rejected`
- source placement remains active
- move request becomes `rejected`

#### Request amendment

If content needs editing, create fact revision proposal.

---

## 9.5.2 Same person owns source and target

New edge case due to updated requirements:

```text
Alice owns Module A.
Alice owns Module B.
Alice moves fact from A to B.
```

Alice may be able to approve both sides.

Recommended default:

```text
Allow it, but log it as self-move.
```

Optional stricter policy:

```text
require_independent_move_review = true
```

If enabled:

- same person cannot be sole approver for both source and target
- another owner/admin must approve one side

---

## 9.5.3 Admin + owner in moves

If Alice is admin and owns both modules:

```text
role_used_source: module_owner
role_used_target: module_owner
```

If Alice owns source but not target:

```text
source role: module_owner
target role: admin_override
```

If Alice owns neither:

```text
source role: admin_override
target role: admin_override
reason: mandatory
```

---

# 9.6 Adding New Modules

Admin creates a new module.

Effects:

- module can receive proposals
- module can receive facts
- module can receive inbound moves

Important:

If existing facts are anchored inside paths now owned by the new module, do not automatically move them.

Instead:

1. Run reconciliation scan.
2. Generate transfer suggestions.
3. Create outbound move proposals.
4. Let owners approve.

Example:

Before:

```text
Module: backend
Path: services/**
```

New module:

```text
Module: auth
Path: services/auth/**
```

Facts anchored under `services/auth/**` should be suggested for transfer to `auth`, not silently moved.

---

# 9.7 Removing / Decommissioning Modules

Module removal must be a workflow, not a destructive delete.

---

## 9.7.1 Module decommission states

```text
active
read_only
decommissioning
archived
```

---

## 9.7.2 Decommission workflow

### Step 1: Admin starts decommission

```json
{
  "module_id": "module-legacy-auth",
  "successor_module_id": "module-auth",
  "reason": "Legacy auth module replaced."
}
```

Module becomes:

```text
decommissioning
```

---

### Step 2: Block new active content

While decommissioning:

- block new accepted facts unless admin override
- block inbound moves unless admin override
- allow review of existing pending items if needed

---

### Step 3: Inventory active facts

For every active fact in the module:

- inspect anchors
- inspect semantic similarity
- inspect successor module
- create reassignment suggestion

---

### Step 4: Create reassignment proposals

For each fact:

```text
MoveRequest
from = decommissioning module
to = suggested module
status = proposed
```

Target modules receive inbound placement proposals.

---

### Step 5: Resolve all facts

Module cannot be archived until:

- all facts moved
- or archived
- or deprecated
- or moved to `Unassigned`

---

### Step 6: Archive module

When no active facts remain:

```text
Module.status = archived
```

Do not hard delete module record because historical proposals/facts may reference it.

---

## 9.7.3 Unassigned module

Create a special system module:

```text
Unassigned
```

Purpose:

- hold facts with no valid module
- prevent information loss during module removal
- allow admin triage later

This module should be admin-owned.

---

# 10. Handling Codebase Changes

The codebase will change continuously.

The system must support:

- file moves
- file renames
- function renames
- line number shifts
- functionality moving
- deleted files
- split/merged functions
- cross-module movement

---

## 10.1 Repository sync

The system should periodically or webhook-based sync the repo.

For each sync:

1. Fetch latest commit.
2. Store commit metadata.
3. Update file index.
4. Update symbol index.
5. Update content chunk index.
6. Update embeddings if enabled.
7. Re-evaluate affected anchors.

---

## 10.2 Code index contents

### File index

```text
commit_sha
path
blob_sha
size
last_modified_commit
```

### Symbol index

```text
commit_sha
path
symbol_type
symbol_name
qualified_name
signature_hash
start_line
end_line
parent_symbol
```

Use:

- tree-sitter
- LSP-based indexer
- ctags fallback
- language-specific parsers

### Content chunk index

```text
chunk_id
commit_sha
path
start_line
end_line
content_hash
embedding_ref
```

---

## 10.3 Anchor health states

```text
valid
drifted
ambiguous
broken
unresolved
```

---

## 10.4 Anchor repair rules

### Case 1: Line numbers changed

If same file/function/context:

```text
auto-update line range
confidence = high
```

No fact content review needed.

---

### Case 2: File renamed

If blob/content unchanged:

```text
auto-update path
confidence = high
```

---

### Case 3: Function renamed

If signature/body/AST strongly match:

```text
create anchor repair suggestion
```

Auto-update only if confidence is very high and policy allows.

Otherwise owner review.

---

### Case 4: Function moved file

Create:

```text
anchor repair suggestion
```

If new file belongs to another module:

```text
also create module transfer suggestion
```

Do not auto-move fact.

---

### Case 5: File deleted

Anchor becomes:

```text
broken
```

Fact becomes:

```text
needs_review
```

Owners decide:

- re-anchor
- keep as conceptual fact
- deprecate
- archive/reject

---

### Case 6: Functionality split

Anchor may match multiple candidates.

System should create:

```text
ambiguous anchor review
```

Owner may:

- select one anchor
- select multiple anchors
- split fact

---

### Case 7: Functionality moved across modules

Create:

```text
module transfer suggestion
```

Actual move requires target module approval.

---

# 11. Anchoring Strategy

Do not rely on line numbers.

Use layered anchors.

## Anchor priority

From most stable to least stable:

```text
1. Fact identity
2. Qualified symbol name
3. AST query
4. Function signature hash
5. Content snippet hash
6. File blob hash
7. File path
8. Line range
```

Example anchor:

```json
{
  "anchor_type": "symbol",
  "repo_id": "repo-main",
  "path": "services/auth/login.go",
  "symbol_name": "handleLogin",
  "qualified_name": "auth.handleLogin",
  "signature_hash": "sha256:...",
  "start_line": 88,
  "end_line": 120,
  "context_hash": "sha256:...",
  "last_verified_commit": "abc123",
  "confidence": 0.96,
  "status": "valid"
}
```

---

# 12. Governance Policies

Because one person can own multiple modules and can also be admin, governance policies are important.

Recommended configuration model:

```yaml
authorization:
  admin_can_review_any_module: true
  admin_override_requires_reason: true
  admin_can_hard_delete: true
  hard_delete_requires_reason: true

ownership:
  allow_multiple_owners_per_module: true
  allow_one_owner_across_multiple_modules: true
  allow_admin_to_be_module_owner: true

review:
  prevent_self_approval: false
  allow_owner_to_approve_own_proposal: true

moves:
  require_source_owner_approval: true
  require_target_owner_approval: true
  allow_same_person_to_approve_source_and_target: true
  flag_self_moves: true

modules:
  decommission_requires_fact_reassignment: true
  archive_requires_empty_active_facts: true

export:
  include_rejected_by_default: false
  include_deleted_by_default: false
  include_audit_events: true
```

---

## 12.1 Recommended defaults

For a small team:

```text
Allow one person to own multiple modules.
Allow admin to also be owner.
Allow admin override with reason.
Allow same person to approve source and target moves.
Flag self-moves in audit.
Prevent hard delete except admin.
Require reason for admin override.
```

For stricter governance:

```text
prevent_self_approval = true
require_independent_move_review = true
admin_override_requires_reason = true
hard_delete_requires_second_admin = true
```

---

# 13. REST API Design

Use versioned REST:

```text
/api/v1
```

---

## 13.1 Identity and roles

```text
GET /me
```

Response:

```json
{
  "principal_id": "user-alice",
  "name": "Alice",
  "global_role": "admin",
  "module_roles": [
    {
      "module_id": "module-auth",
      "role": "owner"
    },
    {
      "module_id": "module-billing",
      "role": "owner"
    }
  ]
}
```

---

## 13.2 Modules

```text
GET    /modules
POST   /modules
GET    /modules/{module_id}
PATCH  /modules/{module_id}
POST   /modules/{module_id}/owners
DELETE /modules/{module_id}/owners/{principal_id}
POST   /modules/{module_id}/decommission
GET    /modules/{module_id}/facts
GET    /modules/{module_id}/proposals
GET    /modules/{module_id}/tasks
```

---

## 13.3 Proposals

```text
POST   /proposals
GET    /proposals
GET    /proposals/{proposal_id}
POST   /proposals/{proposal_id}/withdraw
POST   /proposals/{proposal_id}/clarify
POST   /proposals/{proposal_id}/review
```

Example review:

```json
{
  "action": "accept_with_edits",
  "comment": "Clarified rate limit scope.",
  "acting_role": "module_owner",
  "use_admin_override": false,
  "edited_fact": {
    "title": "Login endpoint rate limit",
    "body": "The login endpoint allows 5 attempts per minute per user."
  }
}
```

Admin override:

```json
{
  "action": "accept",
  "comment": "Owner unavailable.",
  "acting_role": "admin",
  "use_admin_override": true,
  "reason": "Module owner unavailable"
}
```

---

## 13.4 Facts

```text
GET    /facts
GET    /facts/{fact_id}
GET    /facts/{fact_id}/revisions
POST   /facts/{fact_id}/revisions
POST   /facts/{fact_id}/deprecate
POST   /facts/{fact_id}/archive
POST   /facts/{fact_id}/move
GET    /facts/{fact_id}/anchors
POST   /facts/{fact_id}/anchors/repair
GET    /facts/{fact_id}/history
```

---

## 13.5 Moves

```text
POST   /move-requests
GET    /move-requests/{move_id}
POST   /move-requests/{move_id}/review
```

Example:

```json
{
  "action": "accept",
  "acting_role": "module_owner",
  "use_admin_override": false
}
```

---

## 13.6 Repo and indexing

```text
POST   /repos
GET    /repos
POST   /repos/{repo_id}/sync
GET    /repos/{repo_id}/sync-status
```

---

## 13.7 Exports

```text
POST   /exports
GET    /exports/{export_id}
GET    /exports/{export_id}/download
```

---

# 14. MCP Server Design

The MCP server should expose the same capabilities as REST.

---

## 14.1 MCP resources

Examples:

```text
memory://modules
memory://modules/{module_id}
memory://modules/{module_id}/facts
memory://modules/{module_id}/proposals
memory://facts/{fact_id}
memory://facts/{fact_id}/history
memory://me
```

---

## 14.2 MCP tools

```text
get_my_roles
list_modules
get_module
list_module_facts
get_fact
get_fact_history
propose_fact
list_proposals
get_proposal
review_proposal
request_clarification
move_fact
list_move_requests
review_move_request
add_module
decommission_module
export_module
sync_repo
get_anchor_status
```

---

## 14.3 Example: `propose_fact`

Input:

```json
{
  "text": "The auth token expires after 15 minutes.",
  "module_hint": "auth",
  "file_hint": "services/auth/token.go",
  "symbol_hint": "issueToken"
}
```

Output:

```json
{
  "proposal_id": "proposal-123",
  "status": "ready_for_review",
  "mapped_module_id": "module-auth",
  "mapping_confidence": 0.88
}
```

---

## 14.4 Example: `review_proposal`

Input:

```json
{
  "proposal_id": "proposal-123",
  "action": "accept",
  "use_admin_override": false
}
```

If admin override is needed:

```json
{
  "proposal_id": "proposal-123",
  "action": "accept",
  "use_admin_override": true,
  "reason": "Module owner unavailable"
}
```

---

## 14.5 Example: `get_module_context`

Useful for coding agents.

Input:

```json
{
  "module_id": "module-auth",
  "include_facts": true,
  "include_recent_proposals": false,
  "include_deprecated": false
}
```

Output:

```json
{
  "module": "auth",
  "facts": [
    "The login endpoint allows 5 attempts per minute.",
    "Auth tokens expire after 15 minutes."
  ],
  "generated_at": "2026-01-01T00:00:00Z"
}
```

---

# 15. UI Requirements

Even if UI is built later, these views are needed.

---

## 15.1 Personal dashboard

For someone owning multiple modules:

```text
My Modules
- auth
- billing
- notifications

Pending Reviews
- auth: 3
- billing: 1
- notifications: 0

Admin Tasks
- ownerless modules
- decommissioning modules
- unassigned facts
```

---

## 15.2 Module dashboard

For each module:

```text
Module: auth
Status: active
Your access: Owner
Global role: Admin

Tabs:
- Accepted facts
- Proposals
- Acceptance queue
- Move requests
- Anchor health
- Deprecated facts
- Audit log
```

---

## 15.3 Fact detail page

Show:

- current fact body
- current module placement
- historical placements
- revisions
- anchors
- anchor history
- linked proposals
- move history
- review decisions
- role used for approvals
- admin overrides
- export button

---

## 15.4 Role indicator

Always show:

```text
You are acting as: Module Owner
Module: auth
```

or:

```text
You are acting as: Admin Override
Module: payments
Reason required
```

This prevents accidental cross-module actions.

---

# 16. Export Design

Export must cover:

- Proposal Step
- Acceptance Step
- accepted facts
- rejected facts
- archived facts
- move history
- module history
- anchor status
- audit events

---

## 16.1 Export formats

Support:

```text
JSON
JSONL
Markdown
plain text
```

---

## 16.2 Export scopes

Export can be scoped to:

- whole system
- repository
- module
- fact
- proposal queue
- rejected items
- deleted/tombstoned items
- date range

---

## 16.3 JSON export example

```json
{
  "export_version": "1.0",
  "exported_at": "2026-01-01T00:00:00Z",
  "scope": {
    "module_id": "module-auth"
  },
  "modules": [
    {
      "id": "module-auth",
      "name": "auth",
      "status": "active",
      "owners": [
        "user-alice",
        "user-bob"
      ]
    }
  ],
  "facts": [
    {
      "id": "fact-123",
      "title": "Login rate limit",
      "status": "active",
      "placements": [
        {
          "module_id": "module-auth",
          "status": "active"
        }
      ],
      "anchors": [
        {
          "path": "services/auth/login.go",
          "symbol": "handleLogin",
          "status": "valid"
        }
      ],
      "revisions": [
        {
          "version": 1,
          "body": "The login endpoint allows 5 attempts per minute.",
          "approved_by": [
            {
              "principal_id": "user-alice",
              "role_used": "module_owner",
              "admin_override": false
            }
          ]
        }
      ]
    }
  ],
  "proposals": [
    {
      "id": "proposal-1",
      "raw_text": "Login rate limit is 5 per minute.",
      "status": "accepted",
      "mapped_module_id": "module-auth"
    }
  ],
  "events": []
}
```

---

## 16.4 Markdown export example

```markdown
# Module: auth

## Accepted Facts

### Login rate limit

The login endpoint allows 5 attempts per minute.

- Status: active
- Anchor: services/auth/login.go :: handleLogin
- Approved by: Alice as module owner
- Last updated: 2026-01-01

## Rejected Proposals

### Proposal #123

Login rate limit is unlimited.

- Rejected by: Alice
- Role used: module owner
- Reason: Incorrect according to current implementation.
```

---

# 17. Edge Case Matrix

---

## 17.1 Proposal edge cases

| Edge Case | Handling |
|---|---|
| Unknown file path | Create unresolved anchor; do not drop proposal |
| Only module provided | Allow module-level fact without file anchor |
| Only function name provided | Search symbol index |
| Multiple file matches | Mark ambiguous and show candidates |
| Duplicate proposal | Use idempotency key and duplicate detection |
| Proposal contains secrets | Optional scanning/redaction |
| Proposal maps to ownerless module | Route to admin triage |
| Proposal submitted by agent | Apply rate limits and validation |

---

## 17.2 Acceptance edge cases

| Edge Case | Handling |
|---|---|
| No module owner | Admin fallback review |
| Multiple owners disagree | Use policy; escalate to admin if needed |
| Owner edits proposal heavily | Preserve original and edited revision |
| Proposal becomes stale during review | Show anchor health and allow re-mapping |
| Proposal reassigned multiple times | Preserve routing history |
| Admin approves non-owned module | Require admin override reason |
| Admin approves owned module | Prefer recording as module owner |

---

## 17.3 Fact edge cases

| Edge Case | Handling |
|---|---|
| Fact no longer true | Deprecate/archive, not hard delete |
| Anchor broken but fact still conceptually true | Keep fact with `needs_review` anchor |
| Fact applies to multiple modules | Prefer primary module + references |
| Fact partially true | Edit, split, or refine |
| Fact moved but target rejects | Fact remains in source module |
| Fact has pending edit during move | Prefer blocking move until resolved |
| Fact split | Archive original and link successors |
| Fact merged | Keep duplicate as archived with merged_into link |
| Fact hard deleted | Admin-only, reason required, tombstone retained |

---

## 17.4 Module edge cases

| Edge Case | Handling |
|---|---|
| Module paths overlap | Use most specific match and priority |
| Module has no owners | Flag to admin |
| Module removed with facts | Block archive until facts resolved |
| Module removed with pending proposals | Resolve or move proposals |
| New module steals old paths | Generate transfer suggestions |
| Module renamed | Keep stable module ID |
| Module archived | Keep historical record |
| No successor module | Use Unassigned module |

---

## 17.5 Code evolution edge cases

| Edge Case | Handling |
|---|---|
| File renamed | Auto-update path if content unchanged |
| Function renamed | Auto-update if high confidence, else review |
| Function moved file | Create anchor suggestion |
| Line numbers changed | Auto-update using context hash |
| File deleted | Anchor broken; fact needs review |
| Function split | Mark ambiguous; owner may split fact |
| Function merged | Suggest fact merge/update |
| Functionality moved across module | Create transfer suggestion |
| Repo force-push | Treat current commit as new snapshot |
| Unsupported language | Fall back to path/content/embedding anchors |

---

## 17.6 Multi-owner and admin-owner edge cases

| Edge Case | Handling |
|---|---|
| One person owns multiple modules | Provide aggregated dashboard and module context indicators |
| Person approves in wrong module | UI must clearly show active module |
| Person owns source and target of move | Allow by default, flag as self-move; optional independent review |
| Admin approves non-owned module | Record admin override and mandatory reason |
| Admin also owner approves owned module | Record as module owner by default |
| Admin-owner submits and approves own proposal | Configurable; default allow but record; optional prevent_self_approval |
| Admin assigns themselves owner | Allowed but audited |
| Person leaves organization | Transfer module ownerships and admin rights |
| Admin override used often | Report admin override events for governance review |

---

# 18. Audit and Reporting

Because roles can overlap, reporting is important.

## 18.1 Ownership report

```text
Principal: Alice
Global role: Admin
Owned modules:
- auth
- billing
- notifications
```

---

## 18.2 Module coverage report

```text
Module: auth
Owners:
- Alice
- Bob

Module: payments
Owners:
- Bob

Module: infra
Owners:
- none
```

---

## 18.3 Self-move report

```text
Fact: Login rate limit
From: auth
To: billing
Approved by: Alice
Note: Approver owned both source and target modules.
```

---

## 18.4 Admin override report

```text
Admin: Alice
Action: Proposal accepted
Module: payments
Reason: Owner unavailable
```

---

# 19. Security Considerations

## 19.1 Authentication

Support:

- API tokens
- service accounts for agents
- OIDC/OAuth2 later if needed

For MCP:

- local stdio mode may trust local user
- remote HTTP mode should require bearer token

---

## 19.2 Authorization

Enforce permissions in core service layer, not only UI.

Every action should check:

- global role
- module ownership
- target module
- proposal ownership
- admin override permission

---

## 19.3 Rate limiting

Especially for agents:

- proposals per minute
- exports per hour
- search/query limits

---

## 19.4 Idempotency

Agents may retry.

Support:

```http
Idempotency-Key: 123e4567-e89b-12d3-a456-426614174000
```

Or in MCP payload:

```json
{
  "idempotency_key": "agent-run-123"
}
```

---

# 20. Implementation Roadmap

---

## Phase 1: Core Governance MVP

Goal:

> Proposals can be submitted, reviewed, accepted, and exported.

Features:

- principals
- global roles
- modules CRUD
- module owners many-to-many
- proposals CRUD
- manual module mapping
- owner accept/edit/reject
- facts and revisions
- basic placements
- JSON export
- REST API
- basic MCP tools

No advanced code indexing yet.

---

## Phase 2: Code Anchoring

Goal:

> Facts can be connected to files/functions and survive simple changes.

Features:

- repo sync
- file index
- symbol index
- anchor model
- proposal mapping candidates
- anchor status
- manual anchor editing

---

## Phase 3: Code Drift and Repair

Goal:

> System detects when facts become stale.

Features:

- git rename detection
- content hash matching
- symbol drift detection
- anchor repair tasks
- owner review for ambiguous repairs
- fact health flags

---

## Phase 4: Moves and Module Lifecycle

Goal:

> Facts can move safely between modules.

Features:

- move requests
- inbound placement proposals
- target approval
- module decommission workflow
- reassignment suggestions
- Unassigned module
- module archive rules
- self-move logging

---

## Phase 5: AI / Semantic Assistance

Goal:

> Improve mapping and duplicate detection.

Features:

- embeddings
- semantic search
- duplicate suggestions
- conflict detection
- LLM-assisted proposal normalization
- smart module suggestions during reassignment

This should be advisory only.

---

## Phase 6: UI and Operations

Goal:

> Make the system usable by humans.

Features:

- personal dashboard
- proposal inbox
- module dashboard
- fact history browser
- move review UI
- export center
- audit log viewer
- admin override reports
- notifications

---

# 21. Recommended Minimum Viable Product

If you want the simplest useful version, build this first:

## Must have

1. Admin can create modules.
2. Admin can assign multiple owners per module.
3. One person can own multiple modules.
4. Admin can also be owner.
5. Users/agents can submit proposals.
6. System stores raw proposal immutably.
7. Proposal can be manually or automatically mapped to module/file/function.
8. Module owner can:
   - accept
   - edit and accept
   - reject
   - reassign
9. Accepted proposal becomes versioned fact.
10. Fact can be moved via inbound proposal.
11. Target owner must approve inbound move.
12. Module facts can be exported as JSON/Markdown.
13. All actions are logged.
14. Admin override is logged with reason.

## Can wait

1. Full AST indexing.
2. Semantic embeddings.
3. Auto-repair.
4. Conflict detection.
5. Full web UI.

---

# 22. Recommended Default Configuration

If you do not have strong preferences, use these defaults:

```text
Language: Go core + Python indexer worker
Database: PostgreSQL
MCP: Go MCP adapter
Auth: API tokens first, OIDC later
Fact model: globally canonical facts
Module membership: one primary module per fact
Cross-module facts: references, not duplicates
Approval policy: any module owner can approve
Moves: target module owner must approve inbound fact
Module deletion: block until facts reassigned or archived
Rejected proposals: retained in archive
Hard delete: admin-only, tombstone retained
Line numbers: hints only
AI mapping: advisory only, human approval required
Export: JSON + Markdown
Admin behavior: admin can override any module with reason
Admin + owner behavior: act as owner when owner, otherwise admin override
Multi-module owner: allow, but show clear module context
Self-move: allow by default, but flag in audit
Self-approval: allow by default for small teams, optional stricter policy
```

---

# 23. Open Implementation Questions

Before building, decide these:

## Repository

1. Single repo or multiple repos? => Multiple repos.
2. Which programming languages need AST support? => We can skip AST for the time.
3. Do you have Git access from the server? => Yes
4. Sync by webhook, polling, or manual trigger?  
5. Should the system store actual code snippets? => No, just as strings, just in case.

## Ownership

6. Should one owner be enough to approve? => Yes, but it might be possible that we need multiple approvers.
7. Should all owners approve for sensitive modules? => No.
8. Should admin override always require a reason? => Yes
9. Should admins automatically have owner rights in all modules? => No
   - Recommended: no, use explicit override.
10. Should self-approval be prevented? => Yes, but only with a reason.

## Facts

11. Should facts have structured types?
12. Should facts expire? => Not based on TTL. But if the module owner discards it then yes.
13. Should cross-module facts be references or duplicates? => Yes
14. Should rejected proposals remain visible in exports? ==> Yes

## Moves

15. Should same person approve source and target if they own both? => Ye can be possible
16. Should sensitive modules require independent move review? => No
17. Should moves be blocked if fact has pending edits? => No

## UI

18. Do you need a full UI immediately? => No
19. Is API/MCP enough for MVP? => Yes
20. Do you want email/Slack notifications? => No

---

# 24. Final Summary

The final system should be built as:

> A governed, event-sourced knowledge registry where proposals are mapped to code anchors, reviewed by module owners, and maintained as versioned facts with explicit module placements.

The updated ownership requirements are handled by:

1. Many-to-many `ModuleOwner` relationship.
2. Global `admin` role on principal.
3. Effective permission calculation.
4. Explicit recording of `role_used`.
5. Explicit admin override logging.
6. Clear UI context for people owning multiple modules.
7. Governance policies for self-approval and self-move scenarios.

The most important final rules are:

```text
A person can own multiple modules.
A person can be admin and owner simultaneously.
Admin is not silently treated as owner of all modules.
Admin override must be explicit and audited.
If admin is also owner of the target module, default role used should be module owner.
Moves require target module acceptance.
Information is never silently lost.
Everything is exportable and auditable.
```