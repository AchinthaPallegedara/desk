"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { Plus } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Empty, PageHeader } from "@/components/common";
import { CustomerForm } from "@/components/customer-form";
import { CheckboxField } from "@/components/form-controls";
import { useApi, useDebounced } from "@/hooks/use-api";
import type { Customer } from "@/lib/api";

export default function CustomersPage() {
  const router = useRouter();
  const [q, setQ] = useState("");
  const [archived, setArchived] = useState(false);
  const [creating, setCreating] = useState(false);
  const dq = useDebounced(q);
  const { data, loading } = useApi<Customer[]>(`/api/customers?limit=100&q=${encodeURIComponent(dq)}${archived ? "&archived=1" : ""}`);

  return (
    <>
      <PageHeader
        title="Customers"
        actions={
          <Button onClick={() => setCreating(true)}>
            <Plus /> New customer
          </Button>
        }
      />
      <div className="mb-4 flex items-center gap-3">
        <Input placeholder="Search name, company, phone or email…" value={q} onChange={(e) => setQ(e.target.value)} />
        <CheckboxField checked={archived} onCheckedChange={setArchived} className="shrink-0">
          Archived
        </CheckboxField>
      </div>
      {loading && !data ? (
        <div className="flex flex-col gap-2">
          {[0, 1, 2].map((i) => (
            <Skeleton key={i} className="h-14 w-full" />
          ))}
        </div>
      ) : data && data.length > 0 ? (
        <ul className="divide-y rounded-xl border bg-card">
          {data.map((c) => (
            <li key={c.id}>
              <Link href={`/customers/${c.id}`} className="flex flex-col px-4 py-3 hover:bg-muted/50">
                <span className="font-medium">
                  {c.name}
                  {c.archived_at && <span className="ml-2 text-xs font-normal text-muted-foreground">archived</span>}
                </span>
                <span className="truncate text-xs text-muted-foreground">
                  {[c.company, c.phone, c.email].filter(Boolean).join(" · ") || "—"}
                </span>
              </Link>
            </li>
          ))}
        </ul>
      ) : (
        <Empty title={dq ? "No matching customers" : "No customers yet"} />
      )}
      <Dialog open={creating} onOpenChange={setCreating}>
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>New customer</DialogTitle>
          </DialogHeader>
          <CustomerForm initial={{ name: q }} onSaved={(c) => router.push(`/customers/${c.id}`)} />
        </DialogContent>
      </Dialog>
    </>
  );
}
