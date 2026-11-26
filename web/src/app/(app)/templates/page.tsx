"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { toast } from "sonner";
import { Plus } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Field, PageHeader } from "@/components/common";
import { SelectField } from "@/components/form-controls";
import { useApi } from "@/hooks/use-api";
import { api, errorMessage, type TemplateView } from "@/lib/api";
import { relative } from "@/lib/format";

export default function TemplatesPage() {
  const router = useRouter();
  const { data } = useApi<TemplateView[]>("/api/templates");
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const [docType, setDocType] = useState<"estimate" | "invoice">("estimate");
  const [busy, setBusy] = useState(false);

  async function create(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    try {
      const t = await api<TemplateView>("/api/templates", { method: "POST", body: { name, doc_type: docType } });
      router.push(`/templates/${t.id}`);
    } catch (err) {
      toast.error(errorMessage(err));
      setBusy(false);
    }
  }

  return (
    <>
      <PageHeader
        title="Templates"
        description="Typst layouts for your PDFs. The easiest way to design one is to ask your AI assistant."
        actions={
          <Button onClick={() => setOpen(true)}>
            <Plus /> New template
          </Button>
        }
      />
      {(["estimate", "invoice"] as const).map((dt) => (
        <div key={dt} className="mb-6">
          <h2 className="mb-2 text-sm font-semibold capitalize">{dt} templates</h2>
          <ul className="divide-y rounded-xl border bg-card">
            {data
              ?.filter((t) => t.doc_type === dt)
              .map((t) => (
                <li key={t.id}>
                  <Link href={`/templates/${t.id}`} className="flex items-center gap-3 px-4 py-3 hover:bg-muted/50">
                    <span className="flex-1 font-medium">{t.name}</span>
                    {t.is_default && <Badge variant="secondary">Default</Badge>}
                    <span className="text-xs text-muted-foreground">
                      v{t.current_version} · {relative(t.updated_at)}
                    </span>
                  </Link>
                </li>
              ))}
          </ul>
        </div>
      ))}
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent>
          <form onSubmit={create} className="grid gap-4">
            <DialogHeader>
              <DialogTitle>New template</DialogTitle>
              <DialogDescription>Starts from the built-in design; edit the source or ask your AI to restyle it.</DialogDescription>
            </DialogHeader>
            <Field label="Name">
              <Input required value={name} onChange={(e) => setName(e.target.value)} />
            </Field>
            <Field label="Document type">
              <SelectField
                value={docType}
                onValueChange={(v) => setDocType(v as "estimate" | "invoice")}
                options={[
                  { value: "estimate", label: "Estimate" },
                  { value: "invoice", label: "Invoice" },
                ]}
              />
            </Field>
            <DialogFooter>
              <Button type="submit" disabled={busy}>
                {busy ? "Creating…" : "Create"}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </>
  );
}
