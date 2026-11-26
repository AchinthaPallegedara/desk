# Claviq Desk — Project Context

Claviq Desk (desk.claviq.com) is an **AI-native PSA (Professional Services Automation) platform**. It is built first for the owner's own business in Sri Lanka, but designed as **multi-tenant SaaS from day one**.

**The differentiator:** the whole system can be operated by chatting with AI models (Claude, ChatGPT, others) through **MCP**, so the owner can run the business from a phone.

Current state: **Phase 1 implemented** — schema, domain layer, REST API, MCP tools, OAuth 2.1, Typst rendering and web UI. See `deploy/README.md` for production. The source of truth for scope is `AI-Native PSA Platform – Phase 1 Requirements.pdf` (summarised in `docs/`); `docs/data-model.md` supersedes the PDF's ER diagram.

## Repo layout

```
backend/                 Go module github.com/claviq/desk/backend
  cmd/server/            `server` (HTTP) · `migrate <cmd>` · `create-tenant` · `set-password <email>`
  assets/                embedded Poppins fonts + default Typst templates
  internal/config/       env config
  internal/database/     GORM open (UUIDv7 ids), WithTenant() RLS helper, goose runner
  internal/domain/       ALL business logic: customers, settings, templates, fonts, documents
                         (totals, finalise, revise, convert), rendering, signed links, audit
  internal/auth/         argon2id passwords, session cookies, OAuth 2.1 (Fosite) + Postgres store
  internal/render/       sandboxed Typst runner
  internal/storage/      local disk / Cloudflare R2
  internal/httpapi/      REST routes, public preview routes, OAuth routes, middleware, rate limits
  internal/mcpserver/    MCP tools (thin adapters over domain), bearer auth
  migrations/            goose SQL migrations (embedded) — the schema lives here
web/                     Next.js 16 (App Router, src/, Tailwind v4, shadcn base-nova, Poppins)
deploy/                  Caddyfile, systemd units, backup script, prod role SQL, README
docker-compose.yml       local Postgres 18
Makefile                 dev commands
```

## Commands

```
make db-up        # start local Postgres (Docker)
make migrate      # apply migrations (as schema owner)
make backend      # Go API + MCP on :8080
make web          # Next.js on :3000 (proxies /api and /mcp to :8080)
make test / lint
make db-reset     # wipe local DB and re-migrate
make tenant       # create a business + owner login (prompts)
make test         # Go tests incl. DB integration tests (needs db-up + migrate)
```

Env: `backend/.env` (from `.env.example`), `web/.env.local`.

## Database access rules (GORM)

- **Schema = SQL migrations in `backend/migrations/`.** Never use GORM `AutoMigrate`: composite tenant FKs, RLS, partial indexes, deferrable FKs and triggers can't be expressed in GORM tags. GORM structs mirror tables; they don't define them.
- New migration: `backend/migrations/000NN_<name>.sql` with `-- +goose Up` / `-- +goose Down`; wrap functions in `StatementBegin/End`.
- Every tenant table gets: `tenant_id`, `UNIQUE (tenant_id, id)`, `ENABLE` + `FORCE ROW LEVEL SECURITY`, policy `USING (tenant_id = app_current_tenant()) WITH CHECK (same)`, and `set_updated_at` trigger where it has `updated_at`.
- Two DB roles: `claviq` (owner: migrations, create-tenant) and `claviq_app` (runtime, `NOBYPASSRLS`). The server must never connect as the owner.
- Auth tables (`users`, `oauth_clients`, `oauth_tokens`) have no RLS (lookups happen before a tenant is known); only `internal/auth` may query them.
- Finalised documents are protected by DB triggers (SQLSTATE `CQ001` → `domain.ErrLocked`); never work around them.
- **All tenant data access goes through `database.WithTenant(ctx, db, tenantID, func(tx *gorm.DB) error {...})`** and uses only that `tx`. It sets `app.tenant_id` transaction-locally; without it RLS returns nothing.
- Money is `decimal` (use `github.com/shopspring/decimal` in Go), never `float64`.

## MCP rules

- Tools live in `internal/mcpserver`; inputs use plain numbers/strings (no `decimal`/`uuid` types — they break JSON-schema inference); outputs are `any`.
- Every document create/update tool returns `preview_url`; `finalise_document` requires `owner_confirmed=true` and is annotated destructive.
- Update the server `instructions` and `domain.TemplateGuide` when behaviour changes.

## Frontend rules

- `web/AGENTS.md`: Next.js 16 has breaking changes — read `web/node_modules/next/dist/docs/` before using an API you're unsure of.
- UI components: shadcn only (`npx shadcn@latest add <name>` from `web/`; base-nova style = Base UI, so use `render` props, not `asChild`), lucide icons. Mobile-first.
- **No browser-default controls.** Never use raw `<select>`, `<input type="checkbox|radio|date|file|number">`, `<button>` or `<label>` in app code. Use `components/form-controls.tsx` (`SelectField`, `DatePicker`, `CheckboxField`, `RadioField`, `FileButton`), `Button`, `Label`, and `Input inputMode="numeric"` for numbers. The only native input allowed is the hidden one inside `FileButton`.
- The browser talks only to same-origin `/api`; never call the Go service by host from client code.

## Read these before working

| File | What's in it |
|------|--------------|
| `docs/requirements-phase1.md` | Full Phase 1 scope, core use case, out-of-scope list, future phases |
| `docs/architecture.md` | Tech stack, deployment, routing, auth design, Typst rendering, fonts, security/ops |
| `docs/data-model.md` | **Revised** schema (supersedes the PDF's ER): tables, types, constraints, document lifecycle, change log |
| `docs/mcp-tools.md` | MCP tool surface the AI uses, and the rules for AI behaviour |
| `docs/open-questions.md` | Gaps and ambiguities in the spec still to be decided |

## Tech stack (fixed decisions)

- **Backend:** Go — REST API **and** MCP server in **one service**, using the official Go MCP SDK.
- **Database:** PostgreSQL — `tenant_id` on every table, **row-level security** for isolation.
- **Frontend:** Next.js, self-hosted.
- **PDF rendering:** Typst CLI binary (no Docker, no headless browser). Templates = Typst files, data passed as JSON.
- **File storage:** Cloudflare R2 (S3-compatible) — PDFs, logos, tenant fonts.
- **Auth:** OAuth 2.1 built in-house in Go using **Ory Fosite** (no separate auth service). PKCE always.
- **Hosting:** everything on one existing (shared) VPS, behind Caddy or Nginx, one domain, one HTTPS cert.
  - `desk.claviq.com` → Next.js · `/api` → Go REST (optional public) · `/mcp` → MCP endpoint (public)

## Non-negotiable rules

1. **Tenant isolation everywhere.** Every row carries `tenant_id` (except shared fonts and `oauth_clients`). Enforce with Postgres RLS, not just app code. Child tables use composite FKs `(tenant_id, parent_id)`.
2. **One business-logic layer.** The web app (via REST) and MCP tools must call the same Go service/domain code. Never duplicate logic in MCP handlers or in Next.js.
3. **AI never finalises or shares a document without the owner's review.** Finalisation is an explicit, owner-confirmed action.
4. **Final documents are immutable.** On Final: lock the document, freeze snapshots (subtotal, tax, total, terms, item tax_rate), store the PDF. Changes require a new revision.
5. **Numbering is race-safe and gapless.** Drafts have no number. The number is assigned inside the finalise transaction while the `number_sequences` row is locked (`SELECT … FOR UPDATE`). Separate sequences per tenant for estimates and invoices.
6. **Derived values are not stored.** Milestone amounts = `percent × total`. Milestone percentages must sum to exactly 100%.
7. **Add-on items** are priced separately and **excluded from totals**.
8. **Invoice ↔ estimate consistency:** if `invoice.estimate_id` is set, its `customer_id` must equal the estimate's.
9. **Typst is sandboxed.** AI-written templates run with access only to their own template, data and allowed fonts, with a time limit.
10. **Audit everything.** Every mutation (web or AI) writes `audit_logs` with user, via (`web`/`ai`), action, entity, changes JSON.
11. **Templates are versioned.** Each edit creates a new `template_versions` row; documents reference a `template_version_id`; old versions are restorable.
12. **Security basics:** Argon2/bcrypt for passwords, no custom crypto, signed + expiring + unguessable preview links, rate limits on `/api` and `/mcp`.
13. **Currency defaults to LKR.** Taxes (VAT and other Sri Lankan taxes) are configurable per tenant.

## Phase 1 is NOT

No SMS/WhatsApp/email sending (manual only), no online payments/payment links, no projects, no client portal, no public signup, no subscription billing, no roles beyond one owner per tenant. Don't build these — but don't design them out either.

## Guidance

- Optimise for **mobile**: preview pages are reviewed mainly on a phone.
- "Single-tenant in practice, multi-tenant in design" — build tenant structure now; defer signup/billing.
- Keep the VPS footprint light: avoid adding services/containers when a library or binary will do.
- When the spec is ambiguous, check `docs/open-questions.md` and ask rather than guess.
