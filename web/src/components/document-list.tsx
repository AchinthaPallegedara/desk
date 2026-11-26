"use client";

import Link from "next/link";
import { useState } from "react";
import { Plus } from "lucide-react";
import { buttonVariants } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { Empty, PageHeader, StatusBadge } from "@/components/common";
import { SelectField } from "@/components/form-controls";
import { useApi, useDebounced } from "@/hooks/use-api";
import type { DocSummary, Kind } from "@/lib/api";
import { date, docNumber, money } from "@/lib/format";

export function DocumentRows({ docs }: { docs: DocSummary[] }) {
  return (
    <ul className="divide-y rounded-xl border bg-card">
      {docs.map((d) => (
        <li key={d.id}>
          <Link href={`/${d.kind}s/${d.id}`} className="flex items-center gap-3 px-4 py-3 hover:bg-muted/50">
            <div className="min-w-0 flex-1">
              <div className="flex items-center gap-2">
                <span className="truncate font-medium">{d.customer.name}</span>
                <StatusBadge status={d.status} />
              </div>
              <p className="truncate text-xs text-muted-foreground">
                {docNumber(d)}
                {d.title && ` · ${d.title}`}
                {d.issue_date && ` · ${date(d.issue_date)}`}
              </p>
            </div>
            <span className="shrink-0 text-sm font-medium tabular-nums">{money(d.total)}</span>
          </Link>
        </li>
      ))}
    </ul>
  );
}

export function DocumentList({ kind }: { kind: Kind }) {
  const [q, setQ] = useState("");
  const [status, setStatus] = useState("");
  const dq = useDebounced(q);
  const { data, loading, error } = useApi<DocSummary[]>(
    `/api/${kind}s?limit=100&q=${encodeURIComponent(dq)}&status=${status}`,
  );
  const label = kind === "estimate" ? "Estimates" : "Invoices";

  return (
    <>
      <PageHeader
        title={label}
        actions={
          <Link href={`/${kind}s/new`} className={buttonVariants()}>
            <Plus /> New {kind}
          </Link>
        }
      />
      <div className="mb-4 flex gap-2">
        <Input placeholder="Search number, title or customer…" value={q} onChange={(e) => setQ(e.target.value)} />
        <SelectField
          value={status}
          onValueChange={setStatus}
          className="w-36"
          aria-label="Status"
          options={[
            { value: "", label: "Active" },
            { value: "draft", label: "Drafts" },
            { value: "final", label: "Final" },
            { value: "superseded", label: "Superseded" },
          ]}
        />
      </div>
      {error && <p className="text-sm text-destructive">{error}</p>}
      {loading && !data ? (
        <div className="flex flex-col gap-2">
          {[0, 1, 2].map((i) => (
            <Skeleton key={i} className="h-14 w-full" />
          ))}
        </div>
      ) : data && data.length > 0 ? (
        <DocumentRows docs={data} />
      ) : (
        <Empty title={`No ${kind}s yet`}>
          Create one here, or ask your AI assistant: &ldquo;Create an estimate for …&rdquo;
        </Empty>
      )}
    </>
  );
}
