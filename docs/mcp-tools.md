# MCP Tool Surface

Remote MCP server at `desk.claviq.com/mcp`, inside the Go service (official Go MCP SDK). Authenticated via OAuth 2.1; every call is scoped to the token's single user + tenant.

## Tools named in the spec

| Area | Tools |
|------|-------|
| Customers | create customer, find customer |
| Estimates | create estimate, update estimate, list/search estimates |
| Invoices | create invoice, update invoice, list/search invoices |
| Templates | create template, update template |
| Fonts | list fonts |
| Documents | finalise document, get preview link, get PDF link |

Implied by the Phase 1 scope (not explicitly named — confirm before building):
- convert estimate → invoice / per-milestone invoices
- preview template with sample data
- list templates / get template source (needed for AI editing)
- restore template version
- get/update tenant settings, tax rates, bank accounts

## Behaviour rules

- MCP handlers are thin: validate input, call the shared domain layer, format output. **No business logic in handlers.**
- **The AI never finalises or shares a document without the owner's review.** `finalise document` must only run on an explicit owner instruction after they've seen the preview; consider requiring a confirmation token from the preview step.
- Tool responses for documents should include the **signed preview link** so the owner can review on their phone.
- The core flow must work from a **single prompt**: create-or-find customer + create estimate from a saved template.
- Every tool call that mutates data writes `audit_logs` with `via = 'ai'`.
- Rate-limited.

## Skills (optional)

Skills can teach the AI house rules — template style, pricing conventions — and include example Typst templates to improve template quality.
