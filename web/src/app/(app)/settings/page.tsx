"use client";

import { useState, useSyncExternalStore } from "react";
import { toast } from "sonner";
import { Copy, Plus } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Badge } from "@/components/ui/badge";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Field, PageHeader, Section, useConfirm } from "@/components/common";
import { FileButton, SelectField } from "@/components/form-controls";
import { useApi } from "@/hooks/use-api";
import {
  api,
  errorMessage,
  type AuditEntry,
  type BankAccount,
  type Connection,
  type FontView,
  type NumberSequence,
  type Settings,
  type TaxRate,
  type Tenant,
} from "@/lib/api";
import { relative } from "@/lib/format";

export default function SettingsPage() {
  const { data, setData, reload } = useApi<Settings>("/api/settings");
  if (!data) return <p className="text-sm text-muted-foreground">Loading…</p>;
  return (
    <>
      <PageHeader title="Settings" />
      <Tabs defaultValue="business">
        <TabsList className="mb-4 w-full justify-start overflow-x-auto">
          <TabsTrigger value="business">Business</TabsTrigger>
          <TabsTrigger value="billing">Tax & bank</TabsTrigger>
          <TabsTrigger value="numbering">Numbering</TabsTrigger>
          <TabsTrigger value="fonts">Fonts</TabsTrigger>
          <TabsTrigger value="ai">AI & security</TabsTrigger>
        </TabsList>
        <TabsContent value="business">
          <BusinessTab settings={data} onSaved={(t) => setData({ ...data, tenant: t })} reload={reload} />
        </TabsContent>
        <TabsContent value="billing">
          <div className="grid gap-4 lg:grid-cols-2">
            <TaxRates rates={data.tax_rates} reload={reload} />
            <BankAccounts accounts={data.bank_accounts} reload={reload} />
          </div>
        </TabsContent>
        <TabsContent value="numbering">
          <div className="grid gap-4 lg:grid-cols-2">
            {data.number_sequences.map((s) => (
              <SequenceForm key={s.doc_type + s.next_value} seq={s} reload={reload} />
            ))}
          </div>
        </TabsContent>
        <TabsContent value="fonts">
          <Fonts />
        </TabsContent>
        <TabsContent value="ai">
          <AiAndSecurity />
        </TabsContent>
      </Tabs>
    </>
  );
}

// ---------- business ----------

function BusinessTab({ settings, onSaved, reload }: { settings: Settings; onSaved: (t: Tenant) => void; reload: () => void }) {
  const t = settings.tenant;
  const [f, setF] = useState({
    name: t.name,
    legal_name: t.legal_name ?? "",
    reg_no: t.reg_no ?? "",
    tax_reg_no: t.tax_reg_no ?? "",
    address: t.address ?? "",
    phone: t.phone ?? "",
    email: t.email ?? "",
    currency: t.currency,
    default_terms: t.default_terms ?? "",
    default_validity_days: String(t.default_validity_days),
    default_payment_days: String(t.default_payment_days),
  });
  const [busy, setBusy] = useState(false);
  const [logoV, setLogoV] = useState(0);
  const set = (k: keyof typeof f) => (e: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement>) =>
    setF((s) => ({ ...s, [k]: e.target.value }));

  async function save(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    try {
      const nt = await api<Tenant>("/api/settings", {
        method: "PATCH",
        body: {
          ...f,
          default_validity_days: Number(f.default_validity_days),
          default_payment_days: Number(f.default_payment_days),
        },
      });
      onSaved(nt);
      toast.success("Saved");
    } catch (err) {
      toast.error(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  async function uploadLogo(file: File) {
    const fd = new FormData();
    fd.append("file", file);
    try {
      await api("/api/settings/logo", { method: "POST", body: fd });
      setLogoV((v) => v + 1);
      reload();
      toast.success("Logo uploaded");
    } catch (err) {
      toast.error(errorMessage(err));
    }
  }

  async function removeLogo() {
    await api("/api/settings/logo", { method: "DELETE" });
    reload();
  }

  return (
    <div className="grid gap-4 lg:grid-cols-[1fr_300px]">
      <Section title="Business details" description="Printed on every document.">
        <form onSubmit={save} className="grid gap-4 sm:grid-cols-2">
          <Field label="Business name" className="sm:col-span-2">
            <Input required value={f.name} onChange={set("name")} />
          </Field>
          <Field label="Legal name">
            <Input value={f.legal_name} onChange={set("legal_name")} />
          </Field>
          <Field label="Company registration no.">
            <Input value={f.reg_no} onChange={set("reg_no")} />
          </Field>
          <Field label="VAT / TIN">
            <Input value={f.tax_reg_no} onChange={set("tax_reg_no")} />
          </Field>
          <Field label="Currency">
            <Input value={f.currency} maxLength={3} onChange={set("currency")} />
          </Field>
          <Field label="Phone">
            <Input value={f.phone} onChange={set("phone")} />
          </Field>
          <Field label="Email">
            <Input type="email" value={f.email} onChange={set("email")} />
          </Field>
          <Field label="Address" className="sm:col-span-2">
            <Textarea rows={2} value={f.address} onChange={set("address")} />
          </Field>
          <Field label="Estimate validity (days)">
            <Input inputMode="numeric" pattern="[0-9]*" value={f.default_validity_days} onChange={set("default_validity_days")} />
          </Field>
          <Field label="Invoice payment terms (days)">
            <Input inputMode="numeric" pattern="[0-9]*" value={f.default_payment_days} onChange={set("default_payment_days")} />
          </Field>
          <Field label="Default terms and conditions" className="sm:col-span-2">
            <Textarea rows={5} value={f.default_terms} onChange={set("default_terms")} />
          </Field>
          <div className="flex justify-end sm:col-span-2">
            <Button type="submit" disabled={busy}>
              {busy ? "Saving…" : "Save"}
            </Button>
          </div>
        </form>
      </Section>
      <Section title="Logo" description="PNG, JPEG or SVG, up to 2 MB.">
        {settings.has_logo ? (
          // eslint-disable-next-line @next/next/no-img-element
          <img src={`/api/settings/logo?v=${logoV}`} alt="Logo" className="mb-3 max-h-20 rounded border bg-white p-2" />
        ) : (
          <p className="mb-3 text-sm text-muted-foreground">No logo; your business name is shown instead.</p>
        )}
        <div className="flex gap-2">
          <FileButton accept="image/png,image/jpeg,image/svg+xml" onFile={uploadLogo}>
            {settings.has_logo ? "Replace logo" : "Upload logo"}
          </FileButton>
          {settings.has_logo && (
            <Button variant="ghost" onClick={removeLogo}>
              Remove
            </Button>
          )}
        </div>
      </Section>
    </div>
  );
}

// ---------- tax rates ----------

function TaxRates({ rates, reload }: { rates: TaxRate[]; reload: () => void }) {
  const [name, setName] = useState("");
  const [rate, setRate] = useState("");

  async function save(id: string | null, body: Record<string, unknown>) {
    try {
      await api(id ? `/api/tax-rates/${id}` : "/api/tax-rates", { method: id ? "PATCH" : "POST", body });
      reload();
    } catch (e) {
      toast.error(errorMessage(e));
    }
  }

  return (
    <Section title="Tax rates" description="VAT and other taxes. The default applies to new line items.">
      <ul className="mb-4 divide-y text-sm">
        {rates.map((r) => (
          <li key={r.id} className="flex items-center gap-2 py-2">
            <span className={r.active ? "flex-1" : "flex-1 text-muted-foreground line-through"}>
              {r.name} · {r.rate}%
            </span>
            {r.is_default ? (
              <Badge variant="secondary">Default</Badge>
            ) : (
              r.active && (
                <Button variant="ghost" size="xs" onClick={() => save(r.id, { is_default: true })}>
                  Make default
                </Button>
              )
            )}
            <Button variant="ghost" size="xs" onClick={() => save(r.id, { active: !r.active })}>
              {r.active ? "Deactivate" : "Activate"}
            </Button>
          </li>
        ))}
      </ul>
      <form
        className="flex gap-2"
        onSubmit={(e) => {
          e.preventDefault();
          void save(null, { name, rate }).then(() => (setName(""), setRate("")));
        }}
      >
        <Input placeholder="Name (e.g. SSCL)" required value={name} onChange={(e) => setName(e.target.value)} />
        <Input placeholder="%" required inputMode="decimal" value={rate} onChange={(e) => setRate(e.target.value)} className="w-20" />
        <Button type="submit" variant="outline">
          <Plus /> Add
        </Button>
      </form>
    </Section>
  );
}

// ---------- bank accounts ----------

function BankAccounts({ accounts, reload }: { accounts: BankAccount[]; reload: () => void }) {
  const empty = { account_name: "", account_number: "", bank: "", branch: "", swift: "" };
  const [f, setF] = useState(empty);
  const [adding, setAdding] = useState(false);

  async function save(id: string | null, body: Record<string, unknown>) {
    try {
      await api(id ? `/api/bank-accounts/${id}` : "/api/bank-accounts", { method: id ? "PATCH" : "POST", body });
      reload();
      return true;
    } catch (e) {
      toast.error(errorMessage(e));
      return false;
    }
  }

  return (
    <Section
      title="Bank accounts"
      description="The default is printed on documents unless another is chosen."
      actions={
        !adding && (
          <Button variant="outline" size="sm" onClick={() => setAdding(true)}>
            <Plus /> Account
          </Button>
        )
      }
    >
      <ul className="mb-2 divide-y text-sm">
        {accounts.map((b) => (
          <li key={b.id} className="flex items-center gap-2 py-2">
            <div className={b.active ? "flex-1" : "flex-1 text-muted-foreground line-through"}>
              <p className="font-medium">{b.bank}{b.branch && `, ${b.branch}`}</p>
              <p className="text-xs text-muted-foreground">
                {b.account_name} · {b.account_number}
              </p>
            </div>
            {b.is_default ? (
              <Badge variant="secondary">Default</Badge>
            ) : (
              b.active && (
                <Button variant="ghost" size="xs" onClick={() => save(b.id, { is_default: true })}>
                  Make default
                </Button>
              )
            )}
            <Button variant="ghost" size="xs" onClick={() => save(b.id, { active: !b.active })}>
              {b.active ? "Deactivate" : "Activate"}
            </Button>
          </li>
        ))}
        {accounts.length === 0 && <li className="py-2 text-muted-foreground">No bank accounts yet.</li>}
      </ul>
      {adding && (
        <form
          className="grid gap-2 sm:grid-cols-2"
          onSubmit={async (e) => {
            e.preventDefault();
            if (await save(null, { ...f, is_default: accounts.length === 0 })) {
              setF(empty);
              setAdding(false);
            }
          }}
        >
          <Input placeholder="Account name" required value={f.account_name} onChange={(e) => setF({ ...f, account_name: e.target.value })} />
          <Input placeholder="Account number" required value={f.account_number} onChange={(e) => setF({ ...f, account_number: e.target.value })} />
          <Input placeholder="Bank" required value={f.bank} onChange={(e) => setF({ ...f, bank: e.target.value })} />
          <Input placeholder="Branch" value={f.branch} onChange={(e) => setF({ ...f, branch: e.target.value })} />
          <Input placeholder="SWIFT" value={f.swift} onChange={(e) => setF({ ...f, swift: e.target.value })} />
          <div className="flex justify-end gap-2">
            <Button type="button" variant="ghost" onClick={() => setAdding(false)}>
              Cancel
            </Button>
            <Button type="submit">Add</Button>
          </div>
        </form>
      )}
    </Section>
  );
}

// ---------- numbering ----------

function SequenceForm({ seq, reload }: { seq: NumberSequence; reload: () => void }) {
  const [f, setF] = useState({ prefix: seq.prefix, format: seq.format, reset_policy: seq.reset_policy, next_value: String(seq.next_value) });
  const example = f.format
    .replace("{prefix}", f.prefix)
    .replace("{yyyy}", String(new Date().getFullYear()))
    .replace("{yy}", String(new Date().getFullYear()).slice(2))
    .replace("{mm}", String(new Date().getMonth() + 1).padStart(2, "0"))
    .replace(/\{seq(?::(\d+))?\}/, (_, w) => (w ? f.next_value.padStart(Number(w), "0") : f.next_value));

  async function save(e: React.FormEvent) {
    e.preventDefault();
    try {
      await api(`/api/number-sequences/${seq.doc_type}`, { method: "PATCH", body: { ...f, next_value: Number(f.next_value) } });
      toast.success("Saved");
      reload();
    } catch (err) {
      toast.error(errorMessage(err));
    }
  }

  return (
    <Section title={seq.doc_type === "estimate" ? "Estimate numbers" : "Invoice numbers"} description={`Next: ${example}`}>
      <form onSubmit={save} className="grid gap-3 sm:grid-cols-2">
        <Field label="Prefix">
          <Input value={f.prefix} onChange={(e) => setF({ ...f, prefix: e.target.value })} />
        </Field>
        <Field label="Next number">
          <Input inputMode="numeric" pattern="[0-9]*" value={f.next_value} onChange={(e) => setF({ ...f, next_value: e.target.value })} />
        </Field>
        <Field label="Format" hint="{prefix} {yyyy} {yy} {mm} {seq} {seq:4}" className="sm:col-span-2">
          <Input value={f.format} onChange={(e) => setF({ ...f, format: e.target.value })} className="font-mono" />
        </Field>
        <Field label="Reset">
          <SelectField
            value={f.reset_policy}
            onValueChange={(v) => setF({ ...f, reset_policy: v as "never" | "yearly" })}
            options={[
              { value: "yearly", label: "Every year" },
              { value: "never", label: "Never" },
            ]}
          />
        </Field>
        <div className="flex items-end justify-end">
          <Button type="submit">Save</Button>
        </div>
      </form>
    </Section>
  );
}

// ---------- fonts ----------

function Fonts() {
  const { data, reload } = useApi<FontView[]>("/api/fonts");
  const [family, setFamily] = useState("");
  const [busy, setBusy] = useState(false);
  const [up, setUp] = useState({ family: "", weight: "400", style: "normal", licence: "" });
  const [file, setFile] = useState<File | null>(null);

  async function addGoogle(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    try {
      await api("/api/fonts/google", { method: "POST", body: { family } });
      toast.success(`${family} added`);
      setFamily("");
      reload();
    } catch (err) {
      toast.error(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  async function upload(e: React.FormEvent) {
    e.preventDefault();
    if (!file) return;
    const fd = new FormData();
    Object.entries(up).forEach(([k, v]) => fd.append(k, v));
    fd.append("file", file);
    try {
      await api("/api/fonts/upload", { method: "POST", body: fd });
      toast.success("Font uploaded");
      reload();
    } catch (err) {
      toast.error(errorMessage(err));
    }
  }

  return (
    <div className="grid gap-4 lg:grid-cols-2">
      <Section title="Available fonts" description="Shared fonts are open-licence; private fonts are only visible to you.">
        <ul className="divide-y text-sm">
          {data?.map((f) => (
            <li key={f.id} className="py-2">
              <div className="flex items-center gap-2">
                <span className="flex-1 font-medium">{f.family}</span>
                <Badge variant="secondary">{f.scope}</Badge>
              </div>
              <p className="text-xs text-muted-foreground">{f.variants?.join(", ")}</p>
            </li>
          ))}
        </ul>
      </Section>
      <div className="flex flex-col gap-4">
        <Section title="Add from Google Fonts">
          <form onSubmit={addGoogle} className="flex gap-2">
            <Input placeholder="e.g. Inter" required value={family} onChange={(e) => setFamily(e.target.value)} />
            <Button type="submit" variant="outline" disabled={busy}>
              {busy ? "Fetching…" : "Add"}
            </Button>
          </form>
        </Section>
        <Section title="Upload a private font" description="TTF or OTF. The family must match the font's internal name.">
          <form onSubmit={upload} className="grid gap-2 sm:grid-cols-2">
            <Input placeholder="Family" required value={up.family} onChange={(e) => setUp({ ...up, family: e.target.value })} />
            <Input placeholder="Licence (optional)" value={up.licence} onChange={(e) => setUp({ ...up, licence: e.target.value })} />
            <SelectField
              value={up.weight}
              onValueChange={(v) => setUp({ ...up, weight: v })}
              aria-label="Weight"
              options={[100, 200, 300, 400, 500, 600, 700, 800, 900].map((w) => ({ value: String(w), label: `Weight ${w}` }))}
            />
            <SelectField
              value={up.style}
              onValueChange={(v) => setUp({ ...up, style: v })}
              aria-label="Style"
              options={[
                { value: "normal", label: "Normal" },
                { value: "italic", label: "Italic" },
              ]}
            />
            <div className="flex items-center gap-3 sm:col-span-2">
              <FileButton accept=".ttf,.otf" onFile={setFile}>
                Choose font file
              </FileButton>
              <span className="truncate text-sm text-muted-foreground">{file ? file.name : "No file chosen"}</span>
            </div>
            <div className="flex justify-end sm:col-span-2">
              <Button type="submit" variant="outline" disabled={!file}>
                Upload
              </Button>
            </div>
          </form>
        </Section>
      </div>
    </div>
  );
}

// ---------- AI & security ----------

function AiAndSecurity() {
  const { data: conns, reload } = useApi<Connection[]>("/api/auth/connections");
  const { data: audit } = useApi<AuditEntry[]>("/api/audit?limit=30");
  const { confirm, dialog } = useConfirm();
  const origin = useSyncExternalStore(
    () => () => {},
    () => window.location.origin,
    () => "",
  );
  const mcpUrl = origin ? origin + "/mcp" : "";
  const [pw, setPw] = useState({ current_password: "", new_password: "" });

  async function disconnect(c: Connection) {
    const ok = await confirm({
      title: `Disconnect ${c.name}?`,
      description: "It loses access immediately. You can connect it again later.",
      action: "Disconnect",
      destructive: true,
    });
    if (!ok) return;
    await api(`/api/auth/connections/${c.client_id}`, { method: "DELETE" });
    reload();
  }

  async function changePassword(e: React.FormEvent) {
    e.preventDefault();
    try {
      await api("/api/auth/password", { method: "POST", body: pw });
      setPw({ current_password: "", new_password: "" });
      toast.success("Password changed; other devices are signed out");
    } catch (err) {
      toast.error(errorMessage(err));
    }
  }

  return (
    <div className="grid gap-4 lg:grid-cols-2">
      <div className="flex flex-col gap-4">
        <Section title="Connect an AI assistant" description="Add this URL as a custom connector in Claude or ChatGPT, then sign in when asked.">
          <div className="flex gap-2">
            <Input readOnly value={mcpUrl} className="font-mono text-xs" />
            <Button
              variant="outline"
              onClick={() => navigator.clipboard.writeText(mcpUrl).then(() => toast.success("Copied"))}
              aria-label="Copy MCP URL"
            >
              <Copy />
            </Button>
          </div>
        </Section>
        <Section title="Connected assistants">
          {conns && conns.length > 0 ? (
            <ul className="divide-y text-sm">
              {conns.map((c) => (
                <li key={c.client_id} className="flex items-center gap-2 py-2">
                  <div className="flex-1">
                    <p className="font-medium">{c.name}</p>
                    <p className="text-xs text-muted-foreground">
                      Connected {relative(c.connected_at)}
                      {c.last_used_at && ` · last used ${relative(c.last_used_at)}`}
                    </p>
                  </div>
                  <Button variant="ghost" size="sm" onClick={() => disconnect(c)}>
                    Disconnect
                  </Button>
                </li>
              ))}
            </ul>
          ) : (
            <p className="text-sm text-muted-foreground">None connected.</p>
          )}
        </Section>
        <Section title="Change password">
          <form onSubmit={changePassword} className="grid gap-3">
            <Input type="password" placeholder="Current password" autoComplete="current-password" required value={pw.current_password} onChange={(e) => setPw({ ...pw, current_password: e.target.value })} />
            <Input type="password" placeholder="New password (min 10 characters)" autoComplete="new-password" required minLength={10} value={pw.new_password} onChange={(e) => setPw({ ...pw, new_password: e.target.value })} />
            <div className="flex justify-end">
              <Button type="submit" variant="outline">
                Change password
              </Button>
            </div>
          </form>
        </Section>
      </div>
      <Section title="Recent activity" description="Every change, from the web app or your AI.">
        <ul className="divide-y text-sm">
          {audit?.map((a) => (
            <li key={a.id} className="flex items-center gap-2 py-1.5">
              <Badge variant="secondary" className="w-12 justify-center uppercase">
                {a.via}
              </Badge>
              <span className="flex-1 truncate">
                {a.action.replaceAll("_", " ")} {a.entity.replaceAll("_", " ")}
              </span>
              <span className="text-xs text-muted-foreground">{relative(a.created_at)}</span>
            </li>
          ))}
        </ul>
      </Section>
      {dialog}
    </div>
  );
}
