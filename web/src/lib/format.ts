const moneyFmt = new Intl.NumberFormat("en-LK", { minimumFractionDigits: 2, maximumFractionDigits: 2 });

export function money(v: string | number | null | undefined, currency?: string): string {
  if (v === null || v === undefined || v === "") return "—";
  // Accept raw ("618320.00") and pre-formatted ("618,320.00") values.
  const n = typeof v === "number" ? v : Number(String(v).replace(/,/g, ""));
  const s = moneyFmt.format(Number.isFinite(n) ? n : 0);
  return currency ? `${currency} ${s}` : s;
}

const dateFmt = new Intl.DateTimeFormat("en-GB", { day: "2-digit", month: "short", year: "numeric" });

export function date(v: string | null | undefined): string {
  if (!v) return "—";
  const d = new Date(v.length === 10 ? v + "T00:00:00" : v);
  return Number.isNaN(d.getTime()) ? v : dateFmt.format(d);
}

export function relative(v: string): string {
  const diff = (Date.now() - new Date(v).getTime()) / 1000;
  if (diff < 60) return "just now";
  if (diff < 3600) return `${Math.floor(diff / 60)} min ago`;
  if (diff < 86400) return `${Math.floor(diff / 3600)} h ago`;
  if (diff < 86400 * 7) return `${Math.floor(diff / 86400)} d ago`;
  return date(v);
}

export function docLabel(kind: "estimate" | "invoice"): string {
  return kind === "estimate" ? "Estimate" : "Invoice";
}

export function docNumber(d: { number: string | null; revision: number }): string {
  if (!d.number) return "Draft";
  return d.revision > 0 ? `${d.number} · rev ${d.revision}` : d.number;
}

// ---- client-side totals (mirrors backend rounding; the server is authoritative) ----

const r2 = (n: number) => Math.round((n + Number.EPSILON) * 100) / 100;

export interface DraftLine {
  quantity: string;
  unit_price: string;
  discount_type: "none" | "percent" | "amount";
  discount_value: string;
  taxRate: number | null; // percent, or null for no tax
  is_addon: boolean;
}

export function draftTotals(lines: DraftLine[]) {
  let subtotal = 0,
    discount = 0,
    tax = 0,
    addons = 0;
  // Per line: gross = qty × price (the "amount" shown), disc = line discount.
  const lines_: { gross: number; disc: number }[] = [];
  for (const l of lines) {
    const q = Number(l.quantity) || 0;
    const p = Number(l.unit_price) || 0;
    const gross = r2(q * p);
    let d = 0;
    if (l.discount_type === "percent") d = r2((gross * (Number(l.discount_value) || 0)) / 100);
    if (l.discount_type === "amount") d = r2(Number(l.discount_value) || 0);
    d = Math.min(d, gross);
    const net = gross - d;
    const t = l.taxRate !== null ? r2((net * l.taxRate) / 100) : 0;
    lines_.push({ gross, disc: d });
    if (l.is_addon) {
      addons += net + t;
      continue;
    }
    subtotal += gross;
    discount += d;
    tax += t;
  }
  return { subtotal, discount, tax, total: subtotal - discount + tax, addons, lines: lines_ };
}
