# Stroopwafel Social Dashboard — Build Brief (Fleurtje)

## 1) Doel (MVP)
Een lichtgewicht social media dashboard dat:
- posts plant en publiceert naar LinkedIn/Facebook,
- door mensen én AI-agents veilig te gebruiken is,
- direct zicht geeft op status/fouten/reties,
- weinig RAM/CPU gebruikt en simpel te deployen is.

## 2) Productdoelen
- **Vertrouwen**: altijd bewijs wat is gepost en waarom iets faalde.
- **Snelheid**: plannen moet in seconden kunnen, zonder technische kennis.
- **Veiligheid**: API keys gehasht opslaan, duidelijke auth-fouten.
- **Controle**: guardrails tegen spammy planning en kanaal-specifieke regels.
- **Agent-ready**: stabiele JSON API voor bots met duidelijke foutcodes.

## 3) Primaire gebruikers
- **Eigenaar/beheerder**: beheert kanalen, instellingen, API keys.
- **Niet-technische gebruiker**: plant en wijzigt posts via kalender/list.
- **AI-agent**: maakt/werkt posts bij en monitort delivery-status.

## 4) MVP User Stories
1. Als gebruiker wil ik posts plannen in kalender/list, zodat ik overzicht heb.
2. Als gebruiker wil ik per post een history met proof-of-post, zodat ik zeker weet wat live ging.
3. Als gebruiker wil ik waarschuwingen bij slechte timing, zodat ik natuurlijk blijf posten.
4. Als gebruiker wil ik kanaalregels afdwingen (lengte/hashtags/zinnen), zodat posts platformproof zijn.
5. Als gebruiker wil ik duidelijke auth/API foutcategorieën met retry, zodat ik snel herstel.
6. Als gebruiker wil ik een weekly snapshot, zodat ik snel kan bijsturen.
7. Als beheerder wil ik bot-handoff met API key, zodat een agent veilig kan koppelen.

## 5) Acceptance Criteria per MVP-feature

### A. Proof-of-post log
- Iedere poging heeft: status, tijd, kanaal, `external_id`, `permalink`, `error_category`, fouttekst.
- UI toont post history overzichtelijk.
- API levert attempts op via post-endpoint.
- Optioneel screenshot-URL kan achteraf aan poging gekoppeld worden.

### B. Slimme planning guardrails
- Bij create/update/reschedule komt waarschuwing bij exact zelfde timeslot.
- Waarschuwing bij te korte interval (<30 min) t.o.v. nabije geplande posts.
- Waarschuwingen blokkeren niet standaard, maar zijn zichtbaar in UI/API response.

### C. Kanaalregels per platform
- Per kanaal configureerbaar: `max_text_length`, `max_hashtags`, `required_phrase`.
- Validatie draait op create/update/reschedule en in scheduler-send pad.
- Overtreding geeft duidelijke foutmelding en failed attempt in log.

### D. Failsafe auth/API issues
- Publisher classificeert errors minimaal in:
  - `auth_expired`
  - `scope_missing`
  - `rate_limited`
  - `validation_error`
  - `upstream_error`
  - `unknown`
- UI/API tonen categorie en bieden 1-klik retry (post/attempt).

### E. Weekly snapshot (basic)
- Endpoint geeft weekcijfers: planned/published/failed/retries + top post.
- Werkt zonder zware analytics stack.
- Resultaat is bruikbaar in UI-widget én agent polling.

## 6) Technische checklist (MVP)
- [x] SQLite migraties inclusief proof fields en channel rules.
- [x] Scheduler retry/backoff met persistente state.
- [x] API auth via Basic Auth + API key (hash opgeslagen).
- [x] UI login + sessiecookie (`HttpOnly`, `SameSite=Lax`).
- [x] JSON logging (structured) naar stdout.
- [x] `/data`-first docker model (db + config persistent buiten container).
- [x] GHCR build workflow.
- [x] Basis tests voor db/scheduler/handlers.

## 7) Niet-doelen (MVP)
- Geen full engagement inbox.
- Geen geavanceerde analytics warehouse.
- Geen multi-tenant RBAC.
- Geen zware SPA frontend.

## 8) Definition of Done
- `go test ./...` groen.
- `go build ./...` groen.
- Container start met alleen port + `/data` mount.
- UI: create/edit/delete/send flow werkt.
- API: core post lifecycle + attempts + guardrails + weekly snapshot werkt.
- Scheduler verwerkt due/retry posts correct.

## 9) Directe vervolgstappen (na MVP)
1. Templatebibliotheek per contenttype.
2. Engagement inbox (comments/mentions).
3. Auto first comment per kanaal.
4. Content mix dashboard + balanswaarschuwingen.
5. Approval matrix + A/B caption tests.
