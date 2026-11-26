"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useMemo, useState } from "react";
import { toast } from "sonner";
import { Copy, Download, ExternalLink, FilePlus2, Plus, Trash2, ArrowRightLeft, GitBranch } from "lucide-react";
import { Button, buttonVariants } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Switch } from "@/components/ui/switch";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { CustomerPicker } from "@/components/customer-picker";
import { DocumentRows } from "@/components/document-list";
import { Field, PageHeader, Section, StatusBadge, useConfirm } from "@/components/common";
import { CheckboxField, DatePicker, RadioField, SelectField } from "@/components/form-controls";
import { Label } from "@/components/ui/label";
import { useApi } from "@/hooks/use-api";
import {
  api,
  errorMessage,
  type CustomerRef,
  type DocumentView,
  type Kind,
  type Links,
  type Settings,
  type TemplateView,
} from "@/lib/api";
import { date, docLabel, docNumber, draftTotals, money } from "@/lib/format";
import { cn } from "@/lib/utils";

// ---------- editor state ----------

interface ItemState {
  key: string;
  description: string;
  quantity: string;
  unit_price: string;
  discount_type: "none" | "percent" | "amount";
  discount_value: string;
  tax: string; // "default" | "none" | tax rate id
  is_addon: boolean;
}

interface MilestoneState {
  key: string;
  name: string;
  percent: string;
  due_date: string;
  note: string;
}

interface HeaderState {
  customer: CustomerRef | null;
  template_id: string;
  bank_account_id: string; // "" = default
  title: string;
  issue_date: string;
  valid_until: string;
  due_date: string;
  terms: string;
  notes: string;
}

const key = () => Math.random().toString(36).slice(2);

// No discount = empty or zero value; the unit picker only says % or currency amount.
function effectiveDiscountType(it: Pick<ItemState, "discount_type" | "discount_value">): ItemState["discount_type"] {
  return Number(it.discount_value) > 0 ? it.discount_type : "none";
}
const blankItem = (addon = false): ItemState => ({
  key: key(),
  description: "",
  quantity: "1",
  unit_price: "",
  discount_type: "percent",
  discount_value: "",
  tax: "default",
  is_addon: addon,
});

function fromDoc(d: DocumentView) {
  const header: HeaderState = {
    customer: d.customer,
    template_id: d.template.id,
    bank_account_id: d.bank_account_id ?? "",
    title: d.title ?? "",
    issue_date: d.issue_date ?? "",
    valid_until: d.valid_until ?? "",
    due_date: d.due_date ?? "",
    terms: d.terms ?? "",
    notes: d.notes ?? "",
  };
  const items: ItemState[] = d.items.map((it) => ({
    key: it.id,
    description: it.description,
    quantity: it.quantity,
    unit_price: it.unit_price,
    discount_type: it.discount_type === "none" ? "percent" : it.discount_type,
    discount_value: it.discount_type === "none" ? "" : it.discount_value,
    tax: it.tax_rate_id ?? "none",
    is_addon: it.is_addon,
  }));
  const milestones: MilestoneState[] = (d.milestones ?? []).map((m) => ({
    key: m.id,
    name: m.name,
    percent: m.percent,
    due_date: m.due_date ?? "",
    note: m.note ?? "",
  }));
  return { header, items, milestones };
}

// ---------- page ----------

export function DocumentPage({ kind, id }: { kind: Kind; id?: string }) {
  const router = useRouter();
  const { data: settings } = useApi<Settings>("/api/settings");
  const { data: templates } = useApi<TemplateView[]>(`/api/templates?doc_type=${kind}`);
  const { data: doc, setData: setDoc, error } = useApi<DocumentView>(id ? `/api/${kind}s/${id}` : null);

  if (error) return <p className="text-sm text-destructive">{error}</p>;
  if (!settings || !templates || (id && !doc)) return <p className="text-sm text-muted-foreground">Loading…</p>;

  if (doc && doc.status !== "draft") return <IssuedView doc={doc} onChange={setDoc} />;

  return (
    <Editor
      key={doc?.updated_at ?? "new"}
      kind={kind}
      doc={doc}
      settings={settings}
      templates={templates}
      onSaved={(d) => {
        if (!doc) router.replace(`/${kind}s/${d.id}`);
        else setDoc(d);
      }}
      onDeleted={() => router.replace(`/${kind}s`)}
    />
  );
}

// ---------- draft editor ----------

function Editor({
  kind,
  doc,
  settings,
  templates,
  onSaved,
  onDeleted,
}: {
  kind: Kind;
  doc?: DocumentView;
  settings: Settings;
  templates: TemplateView[];
  onSaved: (d: DocumentView) => void;
  onDeleted: () => void;
}) {
  const initial = useMemo(() => {
    if (doc) return fromDoc(doc);
    const def = templates.find((t) => t.is_default) ?? templates[0];
    return {
      header: {
        customer: null,
        template_id: def?.id ?? "",
        bank_account_id: "",
        title: "",
        issue_date: "",
        valid_until: "",
        due_date: "",
        terms: settings.tenant.default_terms ?? "",
        notes: "",
      } as HeaderState,
      items: [blankItem()],
      milestones: [] as MilestoneState[],
    };
  }, [doc, templates, settings]);

  const [h, setH] = useState<HeaderState>(initial.header);
  const [items, setItems] = useState<ItemState[]>(initial.items);
  const [milestones, setMilestones] = useState<MilestoneState[]>(initial.milestones);
  const [dirty, setDirty] = useState(!doc);
  const [busy, setBusy] = useState<string | null>(null);
  const { confirm, dialog } = useConfirm();

  const activeRates = settings.tax_rates.filter((t) => t.active);
  const defaultRate = activeRates.find((t) => t.is_default);
  const rateFor = (tax: string): number | null => {
    if (tax === "none") return null;
    if (tax === "default") return defaultRate ? Number(defaultRate.rate) : null;
    const r = settings.tax_rates.find((t) => t.id === tax);
    return r ? Number(r.rate) : null;
  };
  const totals = draftTotals(items.map((it) => ({ ...it, discount_type: effectiveDiscountType(it), taxRate: rateFor(it.tax) })));
  const pctSum = milestones.reduce((s, m) => s + (Number(m.percent) || 0), 0);

  const touch = () => setDirty(true);
  const setHeader = <K extends keyof HeaderState>(k: K, v: HeaderState[K]) => {
    setH((s) => ({ ...s, [k]: v }));
    touch();
  };
  const setItem = (k: string, patch: Partial<ItemState>) => {
    setItems((s) => s.map((it) => (it.key === k ? { ...it, ...patch } : it)));
    touch();
  };
  const setMs = (k: string, patch: Partial<MilestoneState>) => {
    setMilestones((s) => s.map((m) => (m.key === k ? { ...m, ...patch } : m)));
    touch();
  };

  function payload() {
    const body: Record<string, unknown> = {
      customer_id: h.customer?.id,
      template_id: h.template_id || undefined,
      title: h.title,
      terms: h.terms,
      notes: h.notes,
      items: items
        .filter((it) => it.description.trim() !== "" || it.unit_price !== "")
        .map((it) => ({
          description: it.description,
          quantity: it.quantity || "0",
          unit_price: it.unit_price || "0",
          discount_type: effectiveDiscountType(it),
          discount_value: effectiveDiscountType(it) === "none" ? "0" : it.discount_value,
          ...(it.tax === "none" ? { no_tax: true } : it.tax === "default" ? {} : { tax_rate_id: it.tax }),
          ...(kind === "estimate" ? { is_addon: it.is_addon } : {}),
        })),
    };
    if (h.bank_account_id) body.bank_account_id = h.bank_account_id;
    else if (doc) body.clear_bank_account = true;
    if (h.issue_date) body.issue_date = h.issue_date;
    if (kind === "estimate") {
      if (h.valid_until) body.valid_until = h.valid_until;
      body.milestones = milestones.map((m) => ({
        name: m.name,
        percent: m.percent || "0",
        note: m.note,
        ...(m.due_date ? { due_date: m.due_date } : {}),
      }));
    } else if (h.due_date) body.due_date = h.due_date;
    return body;
  }

  async function save(): Promise<DocumentView | null> {
    if (!h.customer) {
      toast.error("Choose a customer first");
      return null;
    }
    setBusy("save");
    try {
      const d = doc
        ? await api<DocumentView>(`/api/${kind}s/${doc.id}`, { method: "PATCH", body: payload() })
        : await api<DocumentView>(`/api/${kind}s`, { method: "POST", body: payload() });
      setDirty(false);
      toast.success("Saved");
      return d;
    } catch (e) {
      toast.error(errorMessage(e));
      return null;
    } finally {
      setBusy(null);
    }
  }

  async function saveAndNotify() {
    const d = await save();
    if (d) onSaved(d);
  }

  async function preview() {
    let target = doc;
    if (dirty || !doc) {
      const d = await save();
      if (!d) return;
      target = d;
      onSaved(d);
    }
    const w = window.open("", "_blank");
    try {
      const l = await api<Links>(`/api/${kind}s/${target!.id}/links`);
      if (w) w.location.href = l.preview_url;
      else window.location.href = l.preview_url;
    } catch (e) {
      w?.close();
      toast.error(errorMessage(e));
    }
  }

  async function finalise() {
    if (!doc) return;
    const ok = await confirm({
      title: `Finalise this ${kind}?`,
      description: "It gets its number and is locked; the PDF is stored. Later changes need a new revision.",
      action: "Finalise",
    });
    if (!ok) return;
    if (dirty && !(await save())) return;
    setBusy("finalise");
    try {
      const d = await api<DocumentView>(`/api/${kind}s/${doc.id}/finalise`, { method: "POST" });
      toast.success(`${docLabel(kind)} ${d.number} issued`);
      onSaved(d);
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setBusy(null);
    }
  }

  async function remove() {
    if (!doc) return;
    const ok = await confirm({
      title: "Delete this draft?",
      description: "This can't be undone.",
      action: "Delete",
      destructive: true,
    });
    if (!ok) return;
    try {
      await api(`/api/${kind}s/${doc.id}`, { method: "DELETE" });
      toast.success("Draft deleted");
      onDeleted();
    } catch (e) {
      toast.error(errorMessage(e));
    }
  }

  const lines = items.filter((i) => !i.is_addon);
  const addons = items.filter((i) => i.is_addon);

  return (
    <>
      <PageHeader
        title={
          <span className="flex items-center gap-2">
            {doc ? `${docLabel(kind)} ${docNumber(doc)}` : `New ${kind}`}
            {doc && <StatusBadge status="draft" />}
          </span>
        }
        description={doc ? `Template: ${doc.template.name} (v${doc.template.version})` : undefined}
        actions={
          <>
            {doc && (
              <Button variant="ghost" size="icon" onClick={remove} aria-label="Delete draft" title="Delete draft">
                <Trash2 />
              </Button>
            )}
            <Button variant="outline" onClick={preview} disabled={busy !== null}>
              <ExternalLink /> Preview
            </Button>
            <Button variant="outline" onClick={saveAndNotify} disabled={busy !== null || (!dirty && !!doc)}>
              {busy === "save" ? "Saving…" : "Save"}
            </Button>
            {doc && (
              <Button onClick={finalise} disabled={busy !== null}>
                {busy === "finalise" ? "Finalising…" : "Finalise"}
              </Button>
            )}
          </>
        }
      />

      {doc?.warnings && doc.warnings.length > 0 && !dirty && (
        <Alert className="mb-4">
          <AlertDescription>
            <ul className="list-disc pl-4">
              {doc.warnings.map((w) => (
                <li key={w}>{w}</li>
              ))}
            </ul>
          </AlertDescription>
        </Alert>
      )}

      <div className="grid gap-4 lg:grid-cols-[1fr_320px]">
        <div className="flex min-w-0 flex-col gap-4">
          <Section title="Customer & details">
            <div className="grid gap-4 sm:grid-cols-2">
              <Field label="Customer" className="sm:col-span-2">
                <CustomerPicker
                  value={h.customer}
                  onChange={(c) => setHeader("customer", c)}
                  disabled={kind === "invoice" && !!doc?.estimate_id}
                />
              </Field>
              <Field label="Title" className="sm:col-span-2">
                <Input value={h.title} onChange={(e) => setHeader("title", e.target.value)} placeholder="e.g. Website redesign" />
              </Field>
              <Field label="Issue date" hint="Blank = date of finalising">
                <DatePicker value={h.issue_date} onChange={(v) => setHeader("issue_date", v)} placeholder="On finalising" />
              </Field>
              {kind === "estimate" ? (
                <Field label="Valid until" hint={`Blank = issue date + ${settings.tenant.default_validity_days} days`}>
                  <DatePicker value={h.valid_until} onChange={(v) => setHeader("valid_until", v)} placeholder="Automatic" />
                </Field>
              ) : (
                <Field label="Due date" hint={`Blank = issue date + ${settings.tenant.default_payment_days} days`}>
                  <DatePicker value={h.due_date} onChange={(v) => setHeader("due_date", v)} placeholder="Automatic" />
                </Field>
              )}
              <Field label="Template">
                <SelectField
                  value={h.template_id}
                  onValueChange={(v) => setHeader("template_id", v)}
                  options={templates.map((t) => ({ value: t.id, label: t.is_default ? `${t.name} (default)` : t.name }))}
                />
              </Field>
              <Field label="Bank account">
                <SelectField
                  value={h.bank_account_id}
                  onValueChange={(v) => setHeader("bank_account_id", v)}
                  options={[
                    { value: "", label: "Default" },
                    ...settings.bank_accounts
                      .filter((b) => b.active)
                      .map((b) => ({ value: b.id, label: `${b.bank} · ${b.account_number}` })),
                  ]}
                />
              </Field>
            </div>
          </Section>

          <Section
            title="Line items"
            actions={
              <Button variant="outline" size="sm" onClick={() => (setItems((s) => [...s, blankItem()]), touch())}>
                <Plus /> Item
              </Button>
            }
          >
            <ItemsEditor
              items={lines}
              kind={kind}
              settings={settings}
              nets={totals.nets}
              allItems={items}
              setItem={setItem}
              remove={(k) => (setItems((s) => s.filter((i) => i.key !== k)), touch())}
            />
          </Section>

          {kind === "estimate" && (
            <Section
              title="Optional add-ons"
              description="Priced separately and not included in the total."
              actions={
                <Button variant="outline" size="sm" onClick={() => (setItems((s) => [...s, blankItem(true)]), touch())}>
                  <Plus /> Add-on
                </Button>
              }
            >
              {addons.length === 0 ? (
                <p className="text-sm text-muted-foreground">No add-ons.</p>
              ) : (
                <ItemsEditor
                  items={addons}
                  kind={kind}
                  settings={settings}
                  nets={totals.nets}
                  allItems={items}
                  setItem={setItem}
                  remove={(k) => (setItems((s) => s.filter((i) => i.key !== k)), touch())}
                />
              )}
            </Section>
          )}

          {kind === "estimate" && (
            <Section
              title="Payment schedule"
              description={milestones.length ? `Total ${pctSum}% — must be 100% to finalise` : "Optional milestones, e.g. 30 / 20 / 20 / 20 / 10"}
              actions={
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => (
                    setMilestones((s) => [
                      ...s,
                      { key: key(), name: "", percent: String(Math.max(0, 100 - pctSum)), due_date: "", note: "" },
                    ]),
                    touch()
                  )}
                >
                  <Plus /> Milestone
                </Button>
              }
            >
              {milestones.length === 0 ? (
                <p className="text-sm text-muted-foreground">No milestones.</p>
              ) : (
                <div className="flex flex-col gap-3">
                  {milestones.map((m) => (
                    <div key={m.key} className="grid grid-cols-[1fr_80px_auto] gap-2 sm:grid-cols-[1fr_80px_150px_1fr_auto]">
                      <Input placeholder="Milestone" value={m.name} onChange={(e) => setMs(m.key, { name: e.target.value })} />
                      <div className="relative">
                        <Input inputMode="decimal" value={m.percent} onChange={(e) => setMs(m.key, { percent: e.target.value })} className="pr-6 text-right" />
                        <span className="pointer-events-none absolute top-1/2 right-2 -translate-y-1/2 text-xs text-muted-foreground">%</span>
                      </div>
                      <Button
                        variant="ghost"
                        size="icon"
                        className="sm:order-last"
                        onClick={() => (setMilestones((s) => s.filter((x) => x.key !== m.key)), touch())}
                        aria-label="Remove milestone"
                      >
                        <Trash2 />
                      </Button>
                      <DatePicker value={m.due_date} onChange={(v) => setMs(m.key, { due_date: v })} placeholder="Due date" className="col-span-2 sm:col-span-1" />
                      <Input placeholder="Note" value={m.note} onChange={(e) => setMs(m.key, { note: e.target.value })} className="col-span-3 sm:col-span-1" />
                    </div>
                  ))}
                  <p className={cn("text-xs", pctSum === 100 ? "text-muted-foreground" : "text-amber-600")}>
                    {pctSum === 100 ? "Adds up to 100%." : `Adds up to ${pctSum}%.`}{" "}
                    {totals.total > 0 && `Amounts are shares of ${money(totals.total)}.`}
                  </p>
                </div>
              )}
            </Section>
          )}

          <Section title="Notes & terms">
            <div className="grid gap-4">
              <Field label="Notes">
                <Textarea rows={3} value={h.notes} onChange={(e) => setHeader("notes", e.target.value)} />
              </Field>
              <Field label="Terms and conditions">
                <Textarea rows={5} value={h.terms} onChange={(e) => setHeader("terms", e.target.value)} />
              </Field>
            </div>
          </Section>
        </div>

        <aside className="lg:sticky lg:top-20 lg:self-start">
          <Section title="Totals">
            <TotalsTable
              currency={settings.tenant.currency}
              subtotal={totals.subtotal}
              discount={totals.discount}
              tax={totals.tax}
              total={totals.total}
              addons={kind === "estimate" ? totals.addons : 0}
            />
            {dirty && doc && <p className="mt-3 text-xs text-muted-foreground">Unsaved changes.</p>}
          </Section>
        </aside>
      </div>
      {dialog}
    </>
  );
}

function ItemsEditor({
  items,
  allItems,
  nets,
  kind,
  settings,
  setItem,
  remove,
}: {
  items: ItemState[];
  allItems: ItemState[];
  nets: number[];
  kind: Kind;
  settings: Settings;
  setItem: (k: string, p: Partial<ItemState>) => void;
  remove: (k: string) => void;
}) {
  const def = settings.tax_rates.find((t) => t.is_default && t.active);
  if (items.length === 0) return <p className="text-sm text-muted-foreground">No items yet.</p>;
  return (
    <div className="flex flex-col divide-y">
      {items.map((it) => {
        const net = nets[allItems.findIndex((x) => x.key === it.key)] ?? 0;
        return (
          <div key={it.key} className="grid grid-cols-12 gap-x-2 gap-y-3 py-4 first:pt-0 last:pb-0">
            <Textarea
              rows={1}
              placeholder="Description"
              value={it.description}
              onChange={(e) => setItem(it.key, { description: e.target.value })}
              className="col-span-11 min-h-8 resize-y"
            />
            <Button variant="ghost" size="icon" onClick={() => remove(it.key)} aria-label="Remove item" className="justify-self-end">
              <Trash2 />
            </Button>
            <Field label="Qty" className="col-span-4 sm:col-span-2">
              <Input inputMode="decimal" value={it.quantity} onChange={(e) => setItem(it.key, { quantity: e.target.value })} className="text-right" />
            </Field>
            <Field label="Unit price" className="col-span-8 sm:col-span-3">
              <Input inputMode="decimal" value={it.unit_price} onChange={(e) => setItem(it.key, { unit_price: e.target.value })} className="text-right" />
            </Field>
            <Field label="Discount" className="col-span-6 sm:col-span-3">
              <div className="flex">
                <Input
                  inputMode="decimal"
                  placeholder="0"
                  value={it.discount_value}
                  onChange={(e) => setItem(it.key, { discount_value: e.target.value })}
                  className="min-w-0 rounded-r-none text-right"
                  aria-label="Discount"
                />
                <SelectField
                  value={it.discount_type === "amount" ? "amount" : "percent"}
                  onValueChange={(v) => setItem(it.key, { discount_type: v as ItemState["discount_type"] })}
                  className="w-auto shrink-0 rounded-l-none border-l-0"
                  aria-label="Discount unit"
                  options={[
                    { value: "percent", label: "%" },
                    { value: "amount", label: settings.tenant.currency },
                  ]}
                />
              </div>
            </Field>
            <Field label="Tax" className="col-span-6 sm:col-span-4">
              <SelectField
                value={it.tax}
                onValueChange={(v) => setItem(it.key, { tax: v })}
                aria-label="Tax"
                options={[
                  { value: "default", label: def ? `${def.name} ${def.rate}% (default)` : "Default (none)" },
                  { value: "none", label: "No tax" },
                  ...settings.tax_rates
                    .filter((t) => it.tax === t.id || (t.active && t.id !== def?.id))
                    .map((t) => ({ value: t.id, label: `${t.name} ${t.rate}%` })),
                ]}
              />
            </Field>
            <div className="col-span-12 flex items-center justify-between gap-2">
              {kind === "estimate" ? (
                <div className="flex items-center gap-2">
                  <Switch
                    id={`addon-${it.key}`}
                    checked={it.is_addon}
                    onCheckedChange={(v) => setItem(it.key, { is_addon: v })}
                  />
                  <Label htmlFor={`addon-${it.key}`} className="text-xs font-normal text-muted-foreground">
                    Optional add-on
                  </Label>
                </div>
              ) : (
                <span />
              )}
              <span className="text-sm">
                <span className="mr-2 text-xs text-muted-foreground">Amount</span>
                <span className="font-medium tabular-nums">{money(net)}</span>
              </span>
            </div>
          </div>
        );
      })}
    </div>
  );
}

function TotalsTable({
  currency,
  subtotal,
  discount,
  tax,
  total,
  addons,
  taxes,
}: {
  currency: string;
  subtotal: number | string;
  discount: number | string;
  tax: number | string;
  total: number | string;
  addons?: number | string;
  taxes?: { name: string; rate: string; amount: string }[];
}) {
  return (
    <dl className="grid grid-cols-[1fr_auto] gap-y-1.5 text-sm">
      <dt className="text-muted-foreground">Subtotal</dt>
      <dd className="text-right tabular-nums">{money(subtotal)}</dd>
      {Number(discount) > 0 && (
        <>
          <dt className="text-muted-foreground">Discount</dt>
          <dd className="text-right tabular-nums">−{money(discount)}</dd>
        </>
      )}
      {taxes && taxes.length > 0 ? (
        taxes.map((t) => (
          <div key={t.name + t.rate} className="contents">
            <dt className="text-muted-foreground">
              {t.name} ({t.rate}%)
            </dt>
            <dd className="text-right tabular-nums">{money(t.amount)}</dd>
          </div>
        ))
      ) : (
        <>
          <dt className="text-muted-foreground">Tax</dt>
          <dd className="text-right tabular-nums">{money(tax)}</dd>
        </>
      )}
      <dt className="mt-1 border-t pt-2 font-semibold">Total</dt>
      <dd className="mt-1 border-t pt-2 text-right text-base font-semibold tabular-nums">{money(total, currency)}</dd>
      {addons !== undefined && Number(addons) > 0 && (
        <>
          <dt className="pt-2 text-xs text-muted-foreground">Add-ons (not in total)</dt>
          <dd className="pt-2 text-right text-xs tabular-nums text-muted-foreground">{money(addons)}</dd>
        </>
      )}
    </dl>
  );
}

// ---------- issued (final / superseded) ----------

function IssuedView({ doc, onChange }: { doc: DocumentView; onChange: (d: DocumentView) => void }) {
  const router = useRouter();
  const [busy, setBusy] = useState(false);
  const [converting, setConverting] = useState(false);
  const kind = doc.kind;

  async function copyLink() {
    try {
      const l = await api<Links>(`/api/${kind}s/${doc.id}/links`);
      await navigator.clipboard.writeText(l.preview_url);
      toast.success("Preview link copied (valid 7 days)");
    } catch (e) {
      toast.error(errorMessage(e));
    }
  }

  async function revise() {
    setBusy(true);
    try {
      const d = await api<DocumentView>(`/api/${kind}s/${doc.id}/revise`, { method: "POST" });
      toast.success(`Revision ${d.revision} created`);
      router.push(`/${kind}s/${d.id}`);
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }

  const lines = doc.items.filter((i) => !i.is_addon);
  const addons = doc.items.filter((i) => i.is_addon);
  const canConvert = kind === "estimate" && doc.status === "final";

  return (
    <>
      <PageHeader
        title={
          <span className="flex items-center gap-2">
            {docLabel(kind)} {docNumber(doc)} <StatusBadge status={doc.status} />
          </span>
        }
        description={
          <>
            <Link href={`/customers/${doc.customer.id}`} className="hover:underline">
              {doc.customer.name}
            </Link>
            {doc.title && ` · ${doc.title}`} · Issued {date(doc.issue_date)}
          </>
        }
        actions={
          <>
            <a href={`/api/${kind}s/${doc.id}/pdf`} className={buttonVariants({ variant: "outline" })}>
              <Download /> PDF
            </a>
            <Button variant="outline" onClick={copyLink}>
              <Copy /> Link
            </Button>
            {doc.status === "final" && (
              <Button variant="outline" onClick={revise} disabled={busy}>
                <GitBranch /> Revise
              </Button>
            )}
            {canConvert && (
              <Button onClick={() => setConverting(true)}>
                <ArrowRightLeft /> Invoice
              </Button>
            )}
          </>
        }
      />

      {doc.status === "superseded" && (
        <Alert className="mb-4">
          <AlertDescription>This revision was replaced by a newer one. It is kept for your records.</AlertDescription>
        </Alert>
      )}

      <div className="grid gap-4 lg:grid-cols-[1fr_320px]">
        <div className="flex min-w-0 flex-col gap-4">
          <Section title="Line items">
            <ReadOnlyItems items={lines} />
          </Section>
          {addons.length > 0 && (
            <Section title="Optional add-ons" description="Not included in the total.">
              <ReadOnlyItems items={addons} />
            </Section>
          )}
          {doc.milestones && doc.milestones.length > 0 && (
            <Section title="Payment schedule">
              <ul className="divide-y text-sm">
                {doc.milestones.map((m) => (
                  <li key={m.id} className="flex items-center gap-3 py-2">
                    <div className="min-w-0 flex-1">
                      <p className="font-medium">{m.name}</p>
                      <p className="text-xs text-muted-foreground">
                        {m.percent}% · due {date(m.due_date)}
                        {m.note && ` · ${m.note}`}
                      </p>
                    </div>
                    {m.invoice_id ? (
                      <Link href={`/invoices/${m.invoice_id}`} className="text-xs text-primary hover:underline">
                        {m.invoice_number ?? "Draft invoice"}
                      </Link>
                    ) : (
                      <span className="text-xs text-muted-foreground">Not invoiced</span>
                    )}
                    <span className="w-28 text-right font-medium tabular-nums">{money(m.amount)}</span>
                  </li>
                ))}
              </ul>
            </Section>
          )}
          {doc.invoices && doc.invoices.length > 0 && (
            <div>
              <h2 className="mb-2 text-sm font-semibold">Invoices from this estimate</h2>
              <DocumentRows docs={doc.invoices} />
            </div>
          )}
          {(doc.notes || doc.terms) && (
            <Section title="Notes & terms">
              {doc.notes && <p className="mb-3 text-sm whitespace-pre-line">{doc.notes}</p>}
              {doc.terms && <p className="text-xs whitespace-pre-line text-muted-foreground">{doc.terms}</p>}
            </Section>
          )}
        </div>
        <aside className="flex flex-col gap-4 lg:sticky lg:top-20 lg:self-start">
          <Section title="Totals">
            <TotalsTable
              currency={doc.currency}
              subtotal={doc.totals.subtotal}
              discount={doc.totals.discount}
              tax={doc.totals.tax}
              total={doc.totals.total}
              taxes={doc.totals.taxes}
              addons={doc.totals.addons_total}
            />
          </Section>
          <Section title="Details">
            <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1.5 text-sm">
              <dt className="text-muted-foreground">Issued</dt>
              <dd>{date(doc.issue_date)}</dd>
              {doc.valid_until && (
                <>
                  <dt className="text-muted-foreground">Valid until</dt>
                  <dd>{date(doc.valid_until)}</dd>
                </>
              )}
              {doc.due_date && (
                <>
                  <dt className="text-muted-foreground">Due</dt>
                  <dd>{date(doc.due_date)}</dd>
                </>
              )}
              {doc.estimate_id && (
                <>
                  <dt className="text-muted-foreground">Estimate</dt>
                  <dd>
                    <Link href={`/estimates/${doc.estimate_id}`} className="text-primary hover:underline">
                      {doc.estimate_number ?? "View"}
                    </Link>
                  </dd>
                </>
              )}
              <dt className="text-muted-foreground">Template</dt>
              <dd>
                {doc.template.name} v{doc.template.version}
              </dd>
            </dl>
          </Section>
        </aside>
      </div>
      {canConvert && converting && (
        <ConvertDialog
          open={converting}
          onOpenChange={setConverting}
          doc={doc}
          onDone={(first) => {
            onChange(doc);
            router.push(`/invoices/${first}`);
          }}
        />
      )}
    </>
  );
}

function ReadOnlyItems({ items }: { items: DocumentView["items"] }) {
  return (
    <ul className="divide-y text-sm">
      {items.map((it) => (
        <li key={it.id} className="flex gap-3 py-2">
          <div className="min-w-0 flex-1">
            <p className="whitespace-pre-line">{it.description}</p>
            <p className="text-xs text-muted-foreground">
              {it.quantity} × {money(it.unit_price)}
              {Number(it.discount_amount) > 0 && ` · −${money(it.discount_amount)}`}
              {it.tax_name ? ` · ${it.tax_name} ${it.tax_rate}%` : " · no tax"}
            </p>
          </div>
          <span className="font-medium tabular-nums">{money(it.net)}</span>
        </li>
      ))}
    </ul>
  );
}

function ConvertDialog({
  open,
  onOpenChange,
  doc,
  onDone,
}: {
  open: boolean;
  onOpenChange: (o: boolean) => void;
  doc: DocumentView;
  onDone: (firstInvoiceId: string) => void;
}) {
  const unbilled = (doc.milestones ?? []).filter((m) => !m.invoice_id);
  const hasInvoices = (doc.invoices ?? []).length > 0;
  const [mode, setMode] = useState<"full" | "milestones">(unbilled.length > 0 ? "milestones" : "full");
  const [milestones, setMilestones] = useState<string[]>(unbilled.slice(0, 1).map((m) => m.id));
  const [addons, setAddons] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  const addonItems = doc.items.filter((i) => i.is_addon);

  const toggle = (list: string[], set: (v: string[]) => void, id: string) =>
    set(list.includes(id) ? list.filter((x) => x !== id) : [...list, id]);

  async function submit() {
    setBusy(true);
    try {
      const body = mode === "full" ? { mode, addon_item_ids: addons } : { mode, milestone_ids: milestones };
      const invs = await api<DocumentView[]>(`/api/estimates/${doc.id}/convert`, { method: "POST", body });
      toast.success(invs.length === 1 ? "Draft invoice created" : `${invs.length} draft invoices created`);
      onOpenChange(false);
      onDone(invs[0].id);
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Create invoice</DialogTitle>
          <DialogDescription>Creates draft invoice(s) from {doc.number}. Review them before finalising.</DialogDescription>
        </DialogHeader>
        <RadioField
          value={mode}
          onValueChange={(v) => setMode(v as "full" | "milestones")}
          options={[
            {
              value: "full",
              label: `One invoice for the whole estimate (${money(doc.totals.total, doc.currency)})`,
              disabled: hasInvoices,
              children:
                mode === "full" && addonItems.length > 0 ? (
                  <div className="ml-6 flex flex-col gap-2">
                    <span className="text-xs text-muted-foreground">Include accepted add-ons:</span>
                    {addonItems.map((a) => (
                      <CheckboxField key={a.id} checked={addons.includes(a.id)} onCheckedChange={() => toggle(addons, setAddons, a.id)}>
                        {a.description} ({money(a.net)})
                      </CheckboxField>
                    ))}
                  </div>
                ) : undefined,
            },
            {
              value: "milestones",
              label: "Invoice milestones",
              disabled: unbilled.length === 0,
              children:
                mode === "milestones" ? (
                  <div className="ml-6 flex flex-col gap-2">
                    {unbilled.map((m) => (
                      <CheckboxField key={m.id} checked={milestones.includes(m.id)} onCheckedChange={() => toggle(milestones, setMilestones, m.id)}>
                        {m.name} — {m.percent}% ({money(m.amount)})
                      </CheckboxField>
                    ))}
                  </div>
                ) : undefined,
            },
          ]}
        />
        <DialogFooter>
          <Button onClick={submit} disabled={busy || (mode === "milestones" && milestones.length === 0)}>
            <FilePlus2 /> {busy ? "Creating…" : "Create draft"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
