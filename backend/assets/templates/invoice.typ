// Claviq Desk — default invoice template.
// Data: data.json (see docs/template-data.md). All money values arrive pre-formatted.
#let data = json("data.json")
#let doc = data.doc
#let issuer = data.issuer
#let customer = data.customer

#let ink = rgb("#111827")
#let muted = rgb("#6b7280")
#let accent = rgb("#1e3a5f")
#let rule = rgb("#e5e7eb")
#let soft = rgb("#f5f7fa")

#set document(title: doc.title + " " + doc.number, author: issuer.name)
#set page(
  paper: "a4",
  margin: (x: 18mm, top: 16mm, bottom: 20mm),
  footer: context [
    #set text(7.5pt, fill: muted)
    #line(length: 100%, stroke: 0.5pt + rule)
    #v(-4pt)
    #issuer.name
    #if issuer.reg_no != "" [ · Reg. No. #issuer.reg_no ]
    #if issuer.tax_reg_no != "" [ · VAT #issuer.tax_reg_no ]
    #h(1fr)
    #doc.number · Page #counter(page).display("1 of 1", both: true)
  ],
  background: if doc.is_draft {
    rotate(-32deg, text(110pt, weight: "bold", fill: rgb(17, 24, 39, 14))[DRAFT])
  },
)
#set text(font: ("Poppins", "Libertinus Serif"), size: 9pt, fill: ink)
#set par(leading: 0.55em, spacing: 0.9em)

#let caption(body) = text(7.5pt, weight: "medium", fill: muted, body)
// Section headings stick to the block that follows them (never orphaned at a page end).
#let section(title) = block(sticky: true, above: 14pt, below: 5pt, text(10pt, weight: "semibold", title))
#let joined(sep, ..parts) = parts.pos().filter(p => p != "").join(sep, default: "")
#let lines(..parts) = parts.pos().filter(p => p != "" and p != none).join(linebreak(), default: [])

// ---------- header ----------
#grid(
  columns: (1fr, auto),
  gutter: 12pt,
  [
    #if issuer.logo != "" {
      image(issuer.logo, height: 15mm)
    } else {
      text(15pt, weight: "semibold", fill: accent, issuer.name)
    }
    #v(4pt)
    #text(8pt, fill: muted, lines(
      if issuer.logo != "" { issuer.name } else { "" },
      issuer.address,
      joined(" · ", issuer.phone, issuer.email),
    ))
  ],
  align(right)[
    #text(22pt, weight: "semibold", fill: accent, doc.title)
    #v(2pt)
    #text(10pt, weight: "medium", doc.number)
    #if doc.revision > 0 [ #text(8pt, fill: muted)[ (rev. #doc.revision)] ]
  ],
)

#v(10pt)
#line(length: 100%, stroke: 0.5pt + rule)
#v(6pt)

// ---------- parties & dates ----------
#grid(
  columns: (1fr, auto),
  gutter: 12pt,
  [
    #caption[Bill to]
    #v(-2pt)
    #text(10.5pt, weight: "semibold", customer.name)
    #if customer.company != "" [ \ #customer.company ]
    #text(8.5pt, fill: muted)[
      #if customer.address != "" [ \ #customer.address ]
      #if customer.phone != "" or customer.email != "" [ \ #joined(" · ", customer.phone, customer.email) ]
      #if customer.tax_reg_no != "" [ \ VAT #customer.tax_reg_no ]
    ]
  ],
  align(right, table(
    columns: 2,
    stroke: none,
    align: (right, left),
    inset: (x: 4pt, y: 2.5pt),
    caption[Date], doc.issue_date,
    ..if doc.due_date != "" { (caption[Due date], doc.due_date) },
    ..if doc.estimate_number != "" { (caption[Estimate ref.], doc.estimate_number) },
    caption[Currency], doc.currency,
  )),
)

#if doc.subject != "" {
  v(8pt)
  text(11pt, weight: "semibold", doc.subject)
}

#v(8pt)

// ---------- line items ----------
#let item-table(rows) = table(
  columns: (auto, 1fr, auto, auto, auto, auto),
  stroke: (x, y) => if y == 0 { (bottom: 0.8pt + accent) } else { (bottom: 0.4pt + rule) },
  fill: (x, y) => if y == 0 { soft },
  inset: (x: 5pt, y: 6pt),
  align: (x, y) => if x >= 2 { right + horizon } else { left + horizon },
  table.header(
    caption[\#], caption[Description], caption[Qty], caption[Unit price], caption[Tax], caption[Amount],
  ),
  ..rows.map(it => (
    text(fill: muted, str(it.no)),
    [#it.description #if it.discount != "" { linebreak(); text(7.5pt, fill: muted)[Discount #it.discount] }],
    it.quantity,
    it.unit_price,
    text(fill: muted, if it.tax != "" { it.tax } else { "—" }),
    text(weight: "medium", it.amount),
  )).flatten(),
)

#item-table(data.items)

// ---------- totals ----------
#v(4pt)
#align(right, block(width: 62mm, table(
  columns: (1fr, auto),
  stroke: none,
  inset: (x: 4pt, y: 3pt),
  align: (left, right),
  [Subtotal], data.totals.subtotal,
  ..if data.totals.discount != "" { ([Discount], [− #data.totals.discount]) },
  ..data.totals.taxes.map(t => ([#t.name (#t.rate%)], t.amount)).flatten(),
  table.hline(stroke: 0.8pt + accent),
  text(weight: "semibold")[Total (#doc.currency)], text(11pt, weight: "semibold", data.totals.total),
)))

// ---------- amount due ----------
#v(6pt)
#align(right, block(fill: soft, inset: (x: 10pt, y: 7pt), radius: 3pt)[
  #text(8pt, fill: muted)[Amount due#if doc.due_date != "" [ by #doc.due_date]]
  #h(10pt)
  #text(12pt, weight: "semibold", fill: accent)[#doc.currency #data.totals.total]
])

// ---------- notes, bank, terms ----------
#if doc.notes != "" {
  section[Notes]
  block(text(8.5pt, doc.notes))
}

#if data.bank != none {
  v(14pt)
  block(breakable: false, fill: soft, inset: 8pt, radius: 3pt, width: 100%)[
    #text(9pt, weight: "semibold")[Bank details]
    #v(2pt)
    #set text(8.5pt)
    #grid(
      columns: (auto, 1fr),
      column-gutter: 14pt,
      row-gutter: 4pt,
      text(fill: muted)[Account name], data.bank.account_name,
      text(fill: muted)[Account number], data.bank.account_number,
      text(fill: muted)[Bank], joined(", ", data.bank.bank, data.bank.branch),
      ..if data.bank.swift != "" { (text(fill: muted)[SWIFT], data.bank.swift) },
    )
  ]
}

#if doc.terms != "" {
  section[Terms and conditions]
  block(text(8pt, fill: rgb("#374151"), doc.terms))
}
