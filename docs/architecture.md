# Architecture

## Stack

| Layer | Choice | Notes |
|-------|--------|-------|
| Backend | Go | REST API **and** MCP server in one service, official Go MCP SDK. Hosted on the existing VPS. |
| Database | PostgreSQL | `tenant_id` on every table; row-level security for isolation. |
| Frontend | Next.js | Self-hosted on the VPS alongside the backend. |
| PDF rendering | Typst | Single lightweight binary; no Docker or headless browser; renders in milliseconds. |
| File storage | Cloudflare R2 | S3-compatible, no egress fees; PDFs, logos, tenant fonts. |
| Auth | OAuth 2.1 | Built in-house in Go with Ory Fosite (no separate auth service). |

## Deployment & routing

Everything runs on **one shared VPS**, under a single domain, behind a reverse proxy (Caddy or Nginx) with one HTTPS certificate.

| Path | Goes to | Public? |
|------|---------|---------|
| `desk.claviq.com` | Next.js web app | Yes |
| `desk.claviq.com/api` | Go REST API (or called privately by Next.js on the VPS) | Optional |
| `desk.claviq.com/mcp` | MCP endpoint in the Go service | Yes — required for Claude and ChatGPT |

The VPS is shared and resource-constrained: prefer libraries and single binaries over extra services.

## Layering (Go service)

```
HTTP (REST handlers)   MCP (tool handlers)
          \               /
        Domain / service layer   ← all business rules live here exactly once
                  |
     Repository (Postgres, RLS via per-request tenant setting)
                  |
     Infra: Typst renderer · R2 storage · audit logger
```

Both transports authenticate, resolve `(user_id, tenant_id)` from the token/session, and pass it into the domain layer. Every DB transaction sets the tenant context (e.g. `SET LOCAL app.tenant_id = …`) so RLS policies apply.

## Authentication (in-house)

- Login and OAuth built into the Go backend.
- Passwords hashed with **Argon2 or bcrypt**; no custom cryptography.
- OAuth 2.1 core via **Ory Fosite** — don't hand-write the protocol.
- **PKCE on every authorisation flow.**
- **Metadata endpoints** (authorization-server / protected-resource metadata) so Claude, ChatGPT and other clients discover login and token URLs automatically.
- **Client registration** (dynamic) so AI clients can register themselves.
- Access + refresh tokens with expiry and revocation; **every token tied to one user and one tenant**.
- Later: 2FA, email verification, password reset, Sign in with Google.

## Document rendering (Typst)

- Templates are Typst files; data passed as **JSON**, so one template produces every document.
- AI writes/edits templates through MCP tools; a skill with example templates improves quality.
- Go backend shells out to Typst to produce the PDF; **PNG or SVG output** can serve as the phone preview.
- Templates must support: header details, line items, milestone payment schedules, optional add-ons, bank details, terms.
- Validation target: a sample based on a real Claviq invoice — clean **two-page A4** output in **Poppins**.
- Rejected alternatives: headless Chrome (too heavy for shared VPS), WeasyPrint (Python service), Maroto (layouts in Go code, not AI-editable).

## Fonts

- Start with one default font: **Poppins**; add others on demand.
- **Shared master library:** Google Fonts or other open-licence fonts. Once fetched for one tenant, available to all (`fonts.tenant_id = NULL`).
- **Private tenant fonts:** custom uploads belong only to the uploading tenant, never shared (paid licences).
- Font registry in the DB (name, source, shared/private, owning tenant). The AI checks it before designing a template (`list fonts` tool).
- Rendering: Typst gets `--font-path` for the shared library **plus** the tenant's own font folder — nothing else.
- Templates list fallback fonts so a missing font never breaks a PDF.

## Security & operations

- **Preview links:** signed, unguessable, expiring — open on a phone without login but can't be guessed.
- **Template sandboxing:** AI-written Typst runs with access only to its own template, data and allowed fonts, with a **time limit** (Typst `--root` scoped to a temp dir, process timeout, no network).
- **Audit log:** every action via AI or web recorded with user, time and change.
- **Template versions:** each edit saved as a new version; restorable.
- **Backups:** daily PostgreSQL backups and R2 storage, stored **off the VPS**.
- **Rate limits** on API and MCP endpoints.
