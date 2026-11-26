"use client";

import { useState } from "react";
import { Plus, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { CustomerForm } from "@/components/customer-form";
import { useApi, useDebounced } from "@/hooks/use-api";
import type { Customer, CustomerRef } from "@/lib/api";

export function CustomerPicker({
  value,
  onChange,
  disabled,
}: {
  value: CustomerRef | null;
  onChange: (c: CustomerRef | null) => void;
  disabled?: boolean;
}) {
  const [q, setQ] = useState("");
  const [creating, setCreating] = useState(false);
  const dq = useDebounced(q);
  const { data } = useApi<Customer[]>(value ? null : `/api/customers?limit=8&q=${encodeURIComponent(dq)}`);

  if (value) {
    return (
      <div className="flex h-10 items-center justify-between gap-2 rounded-lg border px-3">
        <div className="min-w-0 truncate text-sm">
          <span className="font-medium">{value.name}</span>
          {value.company && <span className="text-muted-foreground"> · {value.company}</span>}
        </div>
        {!disabled && (
          <Button type="button" variant="ghost" size="icon-sm" onClick={() => onChange(null)} aria-label="Change customer">
            <X />
          </Button>
        )}
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-2">
      <div className="flex gap-2">
        <Input placeholder="Search customers…" value={q} onChange={(e) => setQ(e.target.value)} className="h-10" autoFocus />
        <Button type="button" variant="outline" className="h-10" onClick={() => setCreating(true)}>
          <Plus /> New
        </Button>
      </div>
      {data && data.length > 0 && (
        <ul className="divide-y rounded-lg border">
          {data.map((c) => (
            <li key={c.id}>
              <Button
                type="button"
                variant="ghost"
                onClick={() => onChange({ id: c.id, name: c.name, company: c.company })}
                className="h-auto w-full flex-col items-start gap-0 rounded-none px-3 py-2 text-left font-normal"
              >
                <span className="font-medium">{c.name}</span>
                <span className="text-xs text-muted-foreground">{[c.company, c.phone, c.email].filter(Boolean).join(" · ") || "—"}</span>
              </Button>
            </li>
          ))}
        </ul>
      )}
      {data && data.length === 0 && dq && <p className="text-xs text-muted-foreground">No match. Create a new customer.</p>}
      <Dialog open={creating} onOpenChange={setCreating}>
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>New customer</DialogTitle>
          </DialogHeader>
          <CustomerForm
            initial={{ name: q }}
            onSaved={(c) => {
              setCreating(false);
              onChange({ id: c.id, name: c.name, company: c.company });
            }}
          />
        </DialogContent>
      </Dialog>
    </div>
  );
}
