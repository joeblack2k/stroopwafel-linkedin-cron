# Phase 3 Launch Plan (Founder-friendly)

## Week-by-week board (6 weeks)

- **Week 1 — Stabilize core**
  - Finish core reliability fixes and replay quality checks.
  - Verify `/data` persistence and restart behavior.
  - Target: publish success >=95%.

- **Week 2 — Pilot setup**
  - Onboard 3 pilot accounts.
  - Validate onboarding docs and first-run flow.
  - Capture friction list and classify by severity.

- **Week 3 — Pilot polish**
  - Resolve top pilot blockers.
  - Improve error clarity in UI/API payloads.
  - Verify retry + replay outcomes end-to-end.

- **Week 4 — Private beta**
  - Open to ~10 accounts.
  - Track KPIs daily, keep scope tight.
  - Only high-impact bug fixes + small UX tweaks.

- **Week 5 — Hardening**
  - Run rollback drill (image + data safety).
  - Tighten monitoring/alerts.
  - Refresh support docs.

- **Week 6 — Public launch**
  - Publish and monitor weekly KPIs.
  - Freeze non-critical feature work.
  - Keep release cadence predictable and low-risk.

## Definition of Done (release-ready)

- Login/session/auth stable.
- Channel connect + test publish works for supported channels.
- Schedule -> send -> history works end-to-end.
- Retry categories and webhook replay visible in API/UI.
- Docs + OpenAPI + error catalog current.
- Rollback path tested and documented.

## Gate checks

- **Gate A (Internal):** no P0/P1, happy-path script passes.
- **Gate B (Pilot):** pilot KPIs met for 2 weeks.
- **Gate C (Public):** beta KPIs met + rollback drill done.
