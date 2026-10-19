# mem-sys — Persistent, Governed Memory for a Changing Codebase

## The Problem

Codebases evolve constantly — files move, functions are renamed, line numbers shift, ownership changes. Critical knowledge (decisions, constraints, behaviors, design rationale) gets trapped in Slack, docs, PR comments, or individual heads. When the code moves, the knowledge is silently lost.

Existing tools (wiki, Notion, code comments) are:
- **Disconnected from code** — no link to files/functions
- **Fragile** — break on refactors and renames
- **Ungoverned** — no approval, no audit, no ownership
- **Static** — no versioning, no history of *why* something changed

## The Solution

mem-sys is a **governed, versioned knowledge registry** where every fact is:

- **Proposed** by humans or AI agents (via REST or MCP)
- **Reviewed & approved** by module owners (RBAC with admin override)
- **Versioned** — edits create immutable revisions, never silent overwrites
- **Anchored** to code via layered metadata (symbol, path, content hash) — not brittle line numbers
- **Placed** in modules with explicit ownership; moves require target-module acceptance
- **Exportable & auditable** — every action logged (who, when, why, role)

## High-Level Requirements (MVP)

| Capability | Description |
|---|---|
| Propose | Users/agents submit facts via HTTP REST or MCP |
| Review | Module owners accept / edit / reject / reassign |
| Version | Accepted facts become immutable revision chains |
| Place | Facts assigned to modules via governed placements |
| Move | Cross-module moves need target owner approval |
| Export | JSON / Markdown per module or system-wide |
| Audit | Append-only event log with role + override recording |
| RBAC | Global admin + many-to-many module ownership |

## High-Level Architecture

```
┌─────────────┐  ┌─────────────┐  ┌─────────────┐
│   HTTP REST │  │   MCP       │  │  (Future    │
│   (chi)     │  │  (stdio)    │  │   UI)       │
└──────┬──────┘  └──────┬──────┘  └──────┬──────┘
       └────────────────┴────────────────┘
                  ┌──▼──┐
                  │ API │
                  │Layer│ AuthN/AuthZ · RBAC · Idempotency
                  └──┬──┘
                     ▼
        ┌──────────────────────────┐
        │   Core Domain Service    │
        │  Proposals · Facts ·     │
        │  Placements · Moves ·    │
        │  Modules · Audit · Export│
        └───────────┬──────────────┘
                    ▼
              ┌─────▴─────┐
              │  SQLite   │  (event log + entities)
              └───────────┘
```

- **Go** core server owns truth, workflow, consistency, and audit
- **SQLite** storage, single file, zero-ops
- **chi** HTTP router; **MCP** stdio adapter shares the same service layer
- Layered anchors (symbol → AST → content → path → line) survive refactors

## Why This Wins (Pitch Points)

1. **Knowledge survives refactors** — Facts are decoupled from file paths and line numbers; anchors auto-repair on renames/moves.
2. **Nothing is silently lost** — Rejected, deprecated, and deleted items are retained (tombstones) with full audit context.
3. **Governance built-in** — Module owners approve facts; admin override is explicit, audited, and reason-required. One person can own many modules and be admin simultaneously without role confusion.
4. **AI-agent ready** — MCP server + idempotent REST lets agents submit and retrieve knowledge safely.
5. **Zero-ops** — Single Go binary + SQLite file. Deploy anywhere.
6. **Exportable** — JSON/Markdown exports for compliance, onboarding, or context injection into coding agents.

## Ideal For

- Teams with AI-assisted development needing a trusted knowledge layer
- Regulated environments requiring audit trails for code decisions
- Large monorepos with multiple module owners and frequent refactors
- Organizations where tribal knowledge is a risk factor

## Stack

- **Go** (core server)
- **SQLite** (storage)
- **chi** (HTTP) + **MCP stdio** (agent protocol)
- Single binary, no external services
