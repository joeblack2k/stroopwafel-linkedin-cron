# Phase 3 Roadmap (Agent-first)

This roadmap starts from current state (`100/100` parity scope met) and focuses on **API for agents first**, then GUI/operator workflows.

## How this plan was built

- Parallel input from multiple planning agents:
  - product milestone design
  - technical decomposition with effort/dependencies
  - launch execution gates and KPI targets
- Final synthesis is adjusted to the current Go monolith (`net/http`, SQLite, scheduler, webhooks, HTMX).

## Delivery Principles

- API-first: agent workflows ship before UI wrappers.
- Keep monolith simple: no premature service split.
- Additive contracts: backward compatible endpoints, stable error codes.
- Operational safety: replay, auditability, and rollback are first-class.

## Milestones

## M3.1 Foundations (API Core)

**Goal:** strengthen observability and control plane for agents.

**Outcomes**
- Agent actions are traceable and exportable.
- Operators can inspect and filter system behavior quickly.

**Tickets**
- `P3-01` Audit events table + write hooks on mutating actions
- `P3-02` `GET /api/v1/audit-events` with pagination/filter
- `P3-03` `GET /api/v1/posts/export` (CSV/JSON, filtered)
- `P3-04` Global publish-attempts list API
- `P3-13` Channel health endpoint (auth status, last success)
- `P3-17` Bulk channel enable/disable API

## M3.2 Reliability & Delivery Controls

**Goal:** improve failure recovery and delivery policy control.

**Outcomes**
- Retry behavior is configurable and predictable.
- Webhook replay/dead-letter workflows are robust.

**Tickets**
- `P3-05` Per-channel retry policy model + CRUD API
- `P3-06` Scheduler honors channel retry policy + rate-limit signals
- `P3-07` Replay filters + idempotent bulk replay
- `P3-08` Webhook dead-letter queue + threshold alerts (API)
- `P3-14` Credential rotation API + validation flow
- `P3-16` Per-channel posting limits (max/day) guardrail

## M3.3 Content Automation

**Goal:** faster and more consistent agent-assisted content production.

**Outcomes**
- Templates and recurring scheduling become reusable primitives.

**Tickets**
- `P3-09` Media metadata/tags API
- `P3-10` Template CRUD API
- `P3-11` Recurring posts API (RRULE-like simplified model)
- `P3-12` Scheduler materialization for recurring posts + guardrails

## M3.4 Insights + UI Follow-through

**Goal:** bring operator UX up to speed with API capabilities.

**Outcomes**
- Operators can manage audit/dead-letter/templates/recurrence in UI.
- Analytics become more actionable for weekly steering.

**Tickets**
- `P3-15` Analytics API: per-channel delivery stats by date range
- `P3-19` UI: audit log + dead-letter viewer
- `P3-20` UI: template + recurring management
- `P3-18` OpenAPI + error catalog updates for all additions

## Ticket Backlog with Effort

| ID | Ticket | Milestone | SP | Hours | Depends On |
|---|---|---:|---:|---:|---|
| P3-01 | Audit events table + hooks | M3.1 | 5 | 20 | - |
| P3-02 | Audit events list API | M3.1 | 3 | 12 | P3-01 |
| P3-03 | Posts export API (CSV/JSON) | M3.1 | 5 | 20 | - |
| P3-04 | Global publish-attempts list API | M3.1 | 3 | 12 | - |
| P3-13 | Channel health endpoint | M3.1 | 3 | 12 | - |
| P3-17 | Bulk channel enable/disable API | M3.1 | 2 | 8 | - |
| P3-05 | Channel retry policy model + API | M3.2 | 5 | 20 | P3-04 |
| P3-06 | Scheduler retry policy execution | M3.2 | 5 | 20 | P3-05 |
| P3-07 | Replay filters + idempotent bulk replay | M3.2 | 2 | 8 | - |
| P3-08 | Dead-letter queue + alert thresholds | M3.2 | 5 | 20 | P3-07 |
| P3-14 | Credential rotation API + validation | M3.2 | 5 | 20 | P3-13 |
| P3-16 | Per-channel max/day guardrail | M3.2 | 3 | 12 | P3-05 |
| P3-09 | Media metadata/tags API | M3.3 | 3 | 12 | - |
| P3-10 | Template CRUD API | M3.3 | 5 | 20 | P3-09 |
| P3-11 | Recurring posts API | M3.3 | 8 | 32 | P3-10 |
| P3-12 | Recurring scheduler materialization | M3.3 | 8 | 32 | P3-11 |
| P3-15 | Channel delivery analytics API | M3.4 | 5 | 20 | P3-01 |
| P3-19 | UI audit + dead-letter viewer | M3.4 | 5 | 20 | P3-01, P3-08 |
| P3-20 | UI template + recurring management | M3.4 | 5 | 20 | P3-10, P3-11 |
| P3-18 | OpenAPI + error catalog updates | M3.4 | 2 | 8 | all relevant APIs |

## Suggested Sprint Sequence (2 weeks)

- **Sprint 1:** `P3-01`, `P3-02`, `P3-04`, `P3-13`, `P3-17`
- **Sprint 2:** `P3-05`, `P3-06`, `P3-07`, `P3-16`
- **Sprint 3:** `P3-08`, `P3-09`, `P3-10`, `P3-15`
- **Sprint 4:** `P3-11`, `P3-12`, `P3-14`, `P3-18`
- **Sprint 5:** `P3-19`, `P3-20` (+ stabilization buffer)

## KPI Targets by Stage

- **Pilot-ready (M3.1 complete):**
  - 3 pilot accounts live
  - 30 scheduled posts
  - publish success >= 95%
  - median publish latency < 5m
  - 0 P0 bugs

- **Private beta (M3.2 + partial M3.3):**
  - 10 active accounts/week
  - 150 scheduled posts/week
  - publish success >= 97%
  - retry-loop rate <= 3%
  - <3 support tickets/week

- **Public launch (M3 done):**
  - 25 active accounts/week
  - 400 scheduled posts/week
  - publish success >= 98%
  - failed attempt rate < 2%
  - uptime >= 99.5%

## Release Gates

- **Gate A (Internal):** happy-path script passes, no P0/P1, monitoring enabled.
- **Gate B (Pilot):** pilot KPI target met for 2 consecutive weeks.
- **Gate C (Public):** beta KPI target met for 2 weeks + rollback drill complete.

## Top Risks and Mitigations

- Platform token differences -> isolate adapter behavior + contract tests.
- Recurrence timezone/DST bugs -> deterministic UTC expansion with explicit timezone storage.
- Audit/attempt table growth -> retention and indexed query paths.
- Dead-letter floods -> capped retries + threshold alerts.
- Export query pressure -> streaming + safe limits.
- Retry misconfiguration -> sane defaults + validation + warnings.
- Guardrail over-blocking -> warn/block mode switch with audit trail.
- UI slowdown on big datasets -> strict pagination and server-side filters.
- API compatibility drift -> additive endpoint strategy + OpenAPI gating.
- Migration risk -> idempotent migrations + rollback playbook.

## Immediate Build Order (next)

1. Ship `P3-01` + `P3-02` first (audit baseline).
2. Ship `P3-05` + `P3-06` second (delivery reliability uplift).
3. Then `P3-10` + `P3-11` for template/recurring automation.
