"use client";

import { useState } from "react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Field } from "@/components/common";
import { api, errorMessage, type Customer } from "@/lib/api";

type Fields = Pick<Customer, "name" | "company" | "phone" | "email" | "address" | "tax_reg_no" | "notes">;

export function CustomerForm({
  customer,
  initial,
  onSaved,
}: {
  customer?: Customer;
  initial?: Partial<Fields>;
  onSaved: (c: Customer) => void;
}) {
  const [f, setF] = useState<Record<keyof Fields, string>>({
    name: customer?.name ?? initial?.name ?? "",
    company: customer?.company ?? "",
    phone: customer?.phone ?? "",
    email: customer?.email ?? "",
    address: customer?.address ?? "",
    tax_reg_no: customer?.tax_reg_no ?? "",
    notes: customer?.notes ?? "",
  });
  const [busy, setBusy] = useState(false);
  const set = (k: keyof Fields) => (e: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement>) =>
    setF((s) => ({ ...s, [k]: e.target.value }));

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    e.stopPropagation();
    setBusy(true);
    try {
      const c = customer
        ? await api<Customer>(`/api/customers/${customer.id}`, { method: "PATCH", body: f })
        : await api<Customer>("/api/customers", { method: "POST", body: f });
      toast.success(customer ? "Customer saved" : "Customer created");
      onSaved(c);
    } catch (err) {
      toast.error(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <form onSubmit={submit} className="grid gap-4 sm:grid-cols-2">
      <Field label="Name" className="sm:col-span-2">
        <Input required value={f.name} onChange={set("name")} />
      </Field>
      <Field label="Company">
        <Input value={f.company} onChange={set("company")} />
      </Field>
      <Field label="Phone">
        <Input type="tel" value={f.phone} onChange={set("phone")} />
      </Field>
      <Field label="Email">
        <Input type="email" value={f.email} onChange={set("email")} />
      </Field>
      <Field label="VAT / TIN">
        <Input value={f.tax_reg_no} onChange={set("tax_reg_no")} />
      </Field>
      <Field label="Address" className="sm:col-span-2">
        <Textarea rows={2} value={f.address} onChange={set("address")} />
      </Field>
      <Field label="Notes" className="sm:col-span-2">
        <Textarea rows={3} value={f.notes} onChange={set("notes")} />
      </Field>
      <div className="flex justify-end sm:col-span-2">
        <Button type="submit" disabled={busy}>
          {busy ? "Saving…" : customer ? "Save customer" : "Create customer"}
        </Button>
      </div>
    </form>
  );
}
