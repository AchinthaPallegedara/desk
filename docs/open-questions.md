# Open Questions

Gaps and ambiguities found while reading the Phase 1 spec. Resolve with the owner and move answers into the relevant doc.

## Data model
Items 1–5 and 8–10 were resolved in the ER review; see `data-model.md` §6. Still open:

6. **Tax rounding:** per line or per document? (Recommendation: compute tax per line, round each line to 2 dp, then sum.)
7. **Multiple taxes per line:** do the owner's invoices charge SSCL alongside VAT? Built as one tax per line (spec). If SSCL is needed, add item-tax child tables before go-live (`data-model.md` §7).

## Product / AI
11. ~~Finalise guardrail~~ — decided: MCP `finalise_document` requires `owner_confirmed=true`, is annotated destructive (clients ask the user before calling), and the owner can also finalise from the preview page or web app.
12. ~~Preview link expiry~~ — decided: 7 days, HMAC-signed, stateless (rotate `APP_SECRET` to revoke all).
13. **Web app scope in Phase 1:** full CRUD for estimates/invoices on web, or is web mainly customers/settings/preview with AI as primary editor?
14. **Font fetching:** does the system fetch Google Fonts automatically on demand, or does the owner/admin add them?

## Ops
15. ~~Reverse proxy~~ — Caddy (`deploy/Caddyfile`).
16. Backup target off-VPS (another R2 bucket? different provider?) and retention.
17. ~~Tooling~~ — Go 1.26, Next.js 16, goose migrations, GORM for queries.
