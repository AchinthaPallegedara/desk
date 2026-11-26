# Phase 1 Requirements

Source: `AI-Native PSA Platform – Phase 1 Requirements.pdf`

## 1. Overview

Claviq Desk (desk.claviq.com) is an AI-native PSA platform, built first for the owner's own business in Sri Lanka and designed as multi-tenant SaaS from day one so it can be offered to other businesses later. The whole system can be operated by chatting with AI models (Claude, ChatGPT and others) through MCP, so the owner can run the business from a phone without a PC.

## 2. Core use case (the Phase 1 acceptance flow)

A client calls with a new requirement while the owner is away from their PC. The owner:

1. Discusses the requirement with the AI on their phone (features, scope, pricing).
2. Asks the AI, in **a single prompt**, to create the customer (if new) and an estimate from a saved template.
3. Receives a **preview link** in the chat and reviews the draft.
4. Requests edits by chatting until the estimate is correct.
5. Downloads the final PDF to the device and sends it to the client manually.

Anything that makes this flow slow or awkward on a phone is a Phase 1 bug.

## 3. Scope

### 3.1 AI template creation
- Create and edit estimate and invoice templates by chatting with an AI model.
- Templates stored as **Typst files with variables** (customer name, document number, date, line items, subtotal, tax, total, …).
- Templates are reusable: values change per document, the template stays the same.
- Preview a template with sample data.

### 3.2 Customer management
- Add, edit, search, view customers: name, company, phone, email, address, notes.
- Available through both the web app and the AI.

### 3.3 Estimates and invoices
- Create from a template + a customer.
- Line items: quantity, unit price, discounts, tax (VAT and other Sri Lankan taxes, configurable per tenant).
- **Optional add-on items** — priced separately, **not included in the total**.
- **Milestone payment schedule** (e.g. 30/20/20/20/10%) with due dates; percentages must total 100%.
- Terms & conditions and validity date (e.g. 30 days) on estimates.
- Automatic numbering per tenant; **separate sequences** for estimates and invoices.
- Statuses: **Draft, Final** (later: Sent, Accepted, Paid).
- Convert an estimate into **an invoice**, or into **one invoice per milestone**.
- Once Final: document locked, PDF stored; changes need a **new revision**.
- Preview link for review; PDF download for manual sending.
- Currency: **LKR** by default.

### 3.4 Out of scope for Phase 1
- Sending by SMS, WhatsApp or email (manual sending only).
- Online payments and payment links.
- Projects, client portal, public signup, subscription billing.

## 4. AI integration
- Remote MCP server (see `mcp-tools.md`).
- REST API underneath; web app and MCP tools share the same business logic.
- OAuth so each user connects their own tenant; the AI only accesses that tenant's data.
- Optional Skills to teach the AI house rules (template style, pricing conventions).
- **AI never finalises or shares a document without the owner's review.**

## 5. Multi-tenancy
- Every record belongs to a tenant; strict isolation enforced **in the database**.
- Tenant settings: business name, logo, address, company registration number, tax details, bank account details, numbering format, default currency, default terms.
- Users belong to a tenant; Phase 1 has **one owner user per tenant**; roles later.

## 9. Future phases (design so these fit, don't build)
- **Phase 2:** SMS via Dialog; WhatsApp messaging; sending documents directly.
- **Phase 3:** Payment gateways (PayHere and other local gateways; later Stripe, PayPal) and payment links.
- **Phase 4:** Projects — convert estimates/invoices to projects; project space with invoices, estimates, requirement docs, uploaded files; AI uses project docs as planning context.
- **Later:** Google Drive export, client portal, time tracking, expenses, reporting.

## 10. Suggestions from the spec
1. Mobile-friendly preview page (review happens mainly on a phone).
2. Rate card of hourly/daily rates in a later phase, once estimate patterns are clear.
3. Single-tenant in practice, multi-tenant in design: build the tenant structure now, delay billing/signup until selling.
