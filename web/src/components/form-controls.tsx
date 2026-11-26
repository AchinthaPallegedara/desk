"use client";

import { useId, useRef, useState } from "react";
import { CalendarIcon, Upload, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Calendar } from "@/components/ui/calendar";
import { Checkbox } from "@/components/ui/checkbox";
import { Label } from "@/components/ui/label";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { cn } from "@/lib/utils";

// ---------- Select ----------

export interface Option {
  value: string;
  label: React.ReactNode;
}

// Base UI treats "" specially; map it to a sentinel internally so callers can use "".
const EMPTY = "__empty__";
const toInner = (v: string) => (v === "" ? EMPTY : v);
const toOuter = (v: string) => (v === EMPTY ? "" : v);

export function SelectField({
  value,
  onValueChange,
  options,
  placeholder,
  className,
  disabled,
  "aria-label": ariaLabel,
}: {
  value: string;
  onValueChange: (value: string) => void;
  options: Option[];
  placeholder?: string;
  className?: string;
  disabled?: boolean;
  "aria-label"?: string;
}) {
  const items = options.map((o) => ({ value: toInner(o.value), label: o.label }));
  return (
    <Select
      items={items}
      value={toInner(value)}
      onValueChange={(v) => v !== null && onValueChange(toOuter(String(v)))}
      disabled={disabled}
    >
      <SelectTrigger className={cn("w-full", className)} aria-label={ariaLabel}>
        <SelectValue placeholder={placeholder} />
      </SelectTrigger>
      <SelectContent>
        {items.map((o) => (
          <SelectItem key={o.value} value={o.value}>
            {o.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

// ---------- Date picker (value: "YYYY-MM-DD" or "") ----------

const pad = (n: number) => String(n).padStart(2, "0");
const toISO = (d: Date) => `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
const fromISO = (s: string) => {
  const [y, m, d] = s.split("-").map(Number);
  return y && m && d ? new Date(y, m - 1, d) : undefined;
};
const display = new Intl.DateTimeFormat("en-GB", { day: "2-digit", month: "short", year: "numeric" });

export function DatePicker({
  value,
  onChange,
  placeholder = "Pick a date",
  className,
}: {
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
  className?: string;
}) {
  const [open, setOpen] = useState(false);
  const selected = value ? fromISO(value) : undefined;
  return (
    <div className={cn("flex", className)}>
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger
          render={
            <Button
              variant="outline"
              className={cn(
                "flex-1 justify-start font-normal",
                !selected && "text-muted-foreground",
                selected && "rounded-r-none",
              )}
            />
          }
        >
          <CalendarIcon className="text-muted-foreground" />
          {selected ? display.format(selected) : placeholder}
        </PopoverTrigger>
        <PopoverContent className="w-auto p-0" align="start">
          <Calendar
            mode="single"
            selected={selected}
            defaultMonth={selected}
            onSelect={(d) => {
              onChange(d ? toISO(d) : "");
              setOpen(false);
            }}
          />
        </PopoverContent>
      </Popover>
      {selected && (
        <Button
          variant="outline"
          size="icon"
          className="rounded-l-none border-l-0"
          onClick={() => onChange("")}
          aria-label="Clear date"
        >
          <X />
        </Button>
      )}
    </div>
  );
}

// ---------- Checkbox with label ----------

export function CheckboxField({
  checked,
  onCheckedChange,
  children,
  className,
}: {
  checked: boolean;
  onCheckedChange: (checked: boolean) => void;
  children: React.ReactNode;
  className?: string;
}) {
  const id = useId();
  return (
    <div className={cn("flex items-center gap-2", className)}>
      <Checkbox id={id} checked={checked} onCheckedChange={(c) => onCheckedChange(c === true)} />
      <Label htmlFor={id} className="font-normal">
        {children}
      </Label>
    </div>
  );
}

// ---------- Radio group ----------

export function RadioField({
  value,
  onValueChange,
  options,
  className,
}: {
  value: string;
  onValueChange: (value: string) => void;
  options: (Option & { disabled?: boolean; children?: React.ReactNode })[];
  className?: string;
}) {
  const base = useId();
  return (
    <RadioGroup value={value} onValueChange={(v) => onValueChange(String(v))} className={cn("gap-3", className)}>
      {options.map((o) => (
        <div key={o.value} className="flex flex-col gap-2">
          <div className={cn("flex items-center gap-2", o.disabled && "opacity-50")}>
            <RadioGroupItem id={`${base}-${o.value}`} value={o.value} disabled={o.disabled} />
            <Label htmlFor={`${base}-${o.value}`} className="font-normal">
              {o.label}
            </Label>
          </div>
          {o.children}
        </div>
      ))}
    </RadioGroup>
  );
}

// ---------- File picker button ----------

export function FileButton({
  accept,
  onFile,
  children,
  variant = "outline",
  className,
}: {
  accept: string;
  onFile: (file: File) => void;
  children?: React.ReactNode;
  variant?: "outline" | "secondary" | "default";
  className?: string;
}) {
  const ref = useRef<HTMLInputElement>(null);
  return (
    <>
      <input
        ref={ref}
        type="file"
        accept={accept}
        className="sr-only"
        tabIndex={-1}
        onChange={(e) => {
          const f = e.target.files?.[0];
          if (f) onFile(f);
          e.target.value = "";
        }}
      />
      <Button type="button" variant={variant} className={className} onClick={() => ref.current?.click()}>
        <Upload /> {children ?? "Choose file"}
      </Button>
    </>
  );
}
