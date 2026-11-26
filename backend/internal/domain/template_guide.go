package domain

// TemplateGuide is served to AI clients (get_template_guide) and mirrored in
// docs/template-data.md. Keep both in sync with RenderData.
const TemplateGuide = `# Claviq Desk template guide

Templates are Typst (https://typst.app/docs) files. One template renders every
document of its type; per-document values arrive in data.json.

## Loading data
    #let data = json("data.json")

## data.json
All money values are PRE-FORMATTED strings ("1,026,600.00"). Dates are
formatted ("26 Nov 2026"). Missing optional values are "" (empty string),
never null — except data.bank, which is none when there is no bank account.

doc:        kind ("estimate"|"invoice"), title ("Estimate"|"Invoice"), subject (document title, may be ""),
            number ("EST-2026-0001", or "DRAFT"), revision (int), is_draft (bool),
            issue_date, valid_until (estimates), due_date (invoices), currency ("LKR"),
            terms (multi-line), notes, estimate_number (invoices created from an estimate)
issuer:     name, legal_name, reg_no, tax_reg_no, address, phone, email,
            logo (file name to pass to image(), or "" when there is no logo)
customer:   name, company, address, phone, email, tax_reg_no
items:      array of { no (int), description, quantity, unit_price, discount ("10%" | "5,000.00" | ""),
            tax ("VAT 18%" | ""), amount (after discount, before tax) }
addons:     same shape as items — optional extras NOT included in the total (estimates only; may be empty)
addons_total: total of add-ons incl. tax ("" when none)
totals:     subtotal, discount ("" when none), taxes: [{ name, rate, amount }], tax, total
milestones: array of { name, note, percent ("30"), amount, due_date } (estimates; may be empty)
bank:       { account_name, account_number, bank, branch, swift } or none

## Sandbox rules
- Only main.typ, data.json and the logo file are readable. No other files.
- No packages: #import "@preview/..." is rejected. Write layouts inline.
- Rendering must finish within 15 seconds.
- Every save is compiled against sample data first; compile errors are returned verbatim.

## Fonts
- Call list_fonts first. Poppins is always available. Use add_google_font for others.
- Always give fallbacks so a missing font never breaks a PDF:
    #set text(font: ("Poppins", "Libertinus Serif"))

## Conventions (house style)
- A4, margins ~18mm, body 9–10pt.
- Show "DRAFT" prominently when doc.is_draft (e.g. a page background watermark).
- Header: logo or business name, contact details; document title, number, revision (if > 0).
- Show customer block, dates, an item table, totals (with each tax line), add-ons (labelled as
  not included in the total), payment schedule (milestones), bank details and terms.
- Page footer: business name, registration/VAT numbers, "number · Page X of Y".
- Use block(sticky: true) on section headings so they never end a page alone.
- Handle long item lists across pages (tables break automatically; repeat table.header).

Start from get_template on the default template and modify it — it already covers every field.`
