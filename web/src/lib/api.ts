// Typed client for the Go API. The browser only ever calls same-origin /api
// (proxied to Go by Next in dev, by Caddy in production).

export type Kind = "estimate" | "invoice";
export type Status = "draft" | "final" | "superseded";

export interface Customer {
  id: string;
  name: string;
  company: string | null;
  phone: string | null;
  email: string | null;
  address: string | null;
  tax_reg_no: string | null;
  notes: string | null;
  archived_at: string | null;
  created_at: string;
  updated_at: string;
}

export interface CustomerRef {
  id: string;
  name: string;
  company?: string | null;
}

export interface DocSummary {
  kind: Kind;
  id: string;
  number: string | null;
  revision: number;
  status: Status;
  title: string | null;
  customer: CustomerRef;
  total: string;
  currency: string;
  issue_date: string | null;
  created_at: string;
  updated_at: string;
}

export interface ItemView {
  id: string;
  description: string;
  quantity: string;
  unit_price: string;
  discount_type: "none" | "percent" | "amount";
  discount_value: string;
  tax_rate_id: string | null;
  tax_name?: string;
  tax_rate?: string;
  is_addon: boolean;
  gross: string;
  discount_amount: string;
  net: string;
  tax: string;
}

export interface MilestoneView {
  id: string;
  name: string;
  note?: string | null;
  percent: string;
  amount: string;
  due_date: string | null;
  invoice_id: string | null;
  invoice_number?: string | null;
}

export interface Totals {
  subtotal: string;
  discount: string;
  tax: string;
  total: string;
  taxes: { name: string; rate: string; amount: string }[];
  addons_total?: string;
}

export interface DocumentView {
  kind: Kind;
  id: string;
  number: string | null;
  revision: number;
  status: Status;
  title: string | null;
  currency: string;
  issue_date: string | null;
  valid_until?: string | null;
  due_date?: string | null;
  terms: string | null;
  notes: string | null;
  customer: CustomerRef;
  template: { id: string; name: string; version: number };
  bank_account_id: string | null;
  estimate_id?: string | null;
  estimate_number?: string | null;
  previous_revision_id?: string | null;
  items: ItemView[];
  milestones?: MilestoneView[];
  totals: Totals;
  invoices?: DocSummary[];
  warnings?: string[];
  finalised_at?: string | null;
  created_at: string;
  updated_at: string;
}

export interface TaxRate {
  id: string;
  name: string;
  rate: string;
  is_default: boolean;
  active: boolean;
}

export interface BankAccount {
  id: string;
  account_name: string;
  account_number: string;
  bank: string;
  branch: string | null;
  swift: string | null;
  is_default: boolean;
  active: boolean;
}

export interface NumberSequence {
  doc_type: Kind;
  prefix: string;
  format: string;
  reset_policy: "never" | "yearly";
  current_period: number | null;
  next_value: number;
}

export interface Tenant {
  id: string;
  name: string;
  legal_name: string | null;
  reg_no: string | null;
  tax_reg_no: string | null;
  address: string | null;
  phone: string | null;
  email: string | null;
  currency: string;
  default_terms: string | null;
  default_validity_days: number;
  default_payment_days: number;
}

export interface Settings {
  tenant: Tenant;
  has_logo: boolean;
  tax_rates: TaxRate[];
  bank_accounts: BankAccount[];
  number_sequences: NumberSequence[];
}

export interface TemplateView {
  id: string;
  name: string;
  doc_type: Kind;
  is_default: boolean;
  current_version: number;
  archived_at?: string | null;
  updated_at: string;
  typst_source?: string;
}

export interface TemplateVersion {
  id: string;
  template_id: string;
  doc_type: Kind;
  version: number;
  typst_source?: string;
  created_via: "web" | "ai" | "system";
  created_at: string;
}

export interface FontView {
  id: string;
  family: string;
  scope: "shared" | "private";
  source: string;
  licence?: string | null;
  variants: string[];
}

export interface Links {
  preview_url: string;
  pdf_url: string;
  expires_at: string;
}

export interface PreviewInfo {
  kind: Kind | "template";
  id: string;
  title: string;
  number: string;
  status: string;
  customer_name?: string;
  total?: string;
  currency?: string;
  pages: number;
  pdf_url: string;
  version: string;
}

export interface Me {
  user: { id: string; email: string; role: string };
  tenant: { id: string; name: string; currency: string };
}

export interface Connection {
  client_id: string;
  name: string;
  connected_at: string;
  last_used_at: string | null;
}

export interface AuditEntry {
  id: number;
  via: "web" | "ai" | "system";
  action: string;
  entity: string;
  entity_id: string | null;
  created_at: string;
}

export class ApiError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
  ) {
    super(message);
  }
}

export async function api<T>(
  path: string,
  init?: Omit<RequestInit, "body"> & { body?: unknown },
): Promise<T> {
  const { body, headers, ...rest } = init ?? {};
  const isForm = body instanceof FormData;
  const res = await fetch(path, {
    ...rest,
    credentials: "same-origin",
    headers: {
      ...(body !== undefined && !isForm ? { "Content-Type": "application/json" } : {}),
      ...headers,
    },
    body: body === undefined ? undefined : isForm ? (body as FormData) : JSON.stringify(body),
  });
  if (res.status === 204) return undefined as T;
  const text = await res.text();
  const data = text ? JSON.parse(text) : undefined;
  if (!res.ok) {
    if (res.status === 401 && typeof window !== "undefined" && !path.startsWith("/api/auth/login")) {
      const next = window.location.pathname + window.location.search;
      if (!window.location.pathname.startsWith("/login") && !window.location.pathname.startsWith("/p/")) {
        // Called outside React (no router here); a full navigation to /login is intended.
        // eslint-disable-next-line @next/next/no-location-assign-relative-destination
        window.location.href = "/login?next=" + encodeURIComponent(next);
      }
    }
    throw new ApiError(res.status, data?.code ?? "error", data?.error ?? res.statusText);
  }
  return data as T;
}

export function errorMessage(e: unknown): string {
  if (e instanceof ApiError) return e.message;
  if (e instanceof Error) return e.message;
  return "Something went wrong";
}
