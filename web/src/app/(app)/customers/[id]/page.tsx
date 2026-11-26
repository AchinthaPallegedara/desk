"use client";

import Link from "next/link";
import { useParams } from "next/navigation";
import { toast } from "sonner";
import { Plus } from "lucide-react";
import { Button, buttonVariants } from "@/components/ui/button";
import { PageHeader, Section } from "@/components/common";
import { CustomerForm } from "@/components/customer-form";
import { DocumentRows } from "@/components/document-list";
import { useApi } from "@/hooks/use-api";
import { api, errorMessage, type Customer, type DocSummary } from "@/lib/api";

export default function CustomerPage() {
  const { id } = useParams<{ id: string }>();
  const { data: c, setData } = useApi<Customer>(`/api/customers/${id}`);
  const { data: estimates } = useApi<DocSummary[]>(`/api/estimates?customer_id=${id}&limit=100`);
  const { data: invoices } = useApi<DocSummary[]>(`/api/invoices?customer_id=${id}&limit=100`);

  if (!c) return <p className="text-sm text-muted-foreground">Loading…</p>;

  async function toggleArchive() {
    try {
      setData(await api<Customer>(`/api/customers/${id}/${c!.archived_at ? "unarchive" : "archive"}`, { method: "POST" }));
    } catch (e) {
      toast.error(errorMessage(e));
    }
  }

  return (
    <>
      <PageHeader
        title={c.name}
        description={c.company ?? undefined}
        actions={
          <>
            <Button variant="ghost" onClick={toggleArchive}>
              {c.archived_at ? "Unarchive" : "Archive"}
            </Button>
            <Link href="/estimates/new" className={buttonVariants({ variant: "outline" })}>
              <Plus /> Estimate
            </Link>
          </>
        }
      />
      <div className="grid gap-4 lg:grid-cols-[1fr_1fr]">
        <Section title="Details">
          <CustomerForm key={c.updated_at} customer={c} onSaved={setData} />
        </Section>
        <div className="flex flex-col gap-4">
          <div>
            <h2 className="mb-2 text-sm font-semibold">Estimates</h2>
            {estimates && estimates.length > 0 ? (
              <DocumentRows docs={estimates} />
            ) : (
              <p className="text-sm text-muted-foreground">None yet.</p>
            )}
          </div>
          <div>
            <h2 className="mb-2 text-sm font-semibold">Invoices</h2>
            {invoices && invoices.length > 0 ? (
              <DocumentRows docs={invoices} />
            ) : (
              <p className="text-sm text-muted-foreground">None yet.</p>
            )}
          </div>
        </div>
      </div>
    </>
  );
}
