"use client";

import { useCallback, useEffect, useState } from "react";
import { useParams } from "next/navigation";
import Link from "next/link";
import { Download, RefreshCw } from "lucide-react";
import { toast } from "sonner";
import { Button, buttonVariants } from "@/components/ui/button";
import { StatusBadge, useConfirm } from "@/components/common";
import { api, errorMessage, type Me, type PreviewInfo } from "@/lib/api";
import { money } from "@/lib/format";
import { cn } from "@/lib/utils";

// Public, phone-first review page for a signed preview link. Opens without
// login; if the owner happens to be signed in, drafts can be finalised here.
export default function PreviewPage() {
  const { token } = useParams<{ token: string }>();
  const [info, setInfo] = useState<PreviewInfo | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [me, setMe] = useState<Me | null>(null);
  const [busy, setBusy] = useState(false);
  const { confirm, dialog } = useConfirm();

  const fetchInfo = useCallback(
    () =>
      api<PreviewInfo>(`/api/public/p/${token}`).then(
        (i) => {
          setInfo(i);
          setError(null);
        },
        (e) => setError(errorMessage(e) === "not found" ? "This link is invalid or has expired." : errorMessage(e)),
      ),
    [token],
  );
  const load = () => void fetchInfo();

  useEffect(() => {
    void fetchInfo();
    fetch("/api/auth/me", { credentials: "same-origin" })
      .then((r) => (r.ok ? r.json() : null))
      .then(setMe)
      .catch(() => {});
  }, [fetchInfo]);

  async function finalise() {
    if (!info || info.kind === "template") return;
    const ok = await confirm({
      title: `Finalise this ${info.kind}?`,
      description: "It gets its number, is locked, and its PDF is stored. Changes after this need a new revision.",
      action: "Finalise",
    });
    if (!ok) return;
    setBusy(true);
    try {
      await api(`/api/${info.kind}s/${info.id}/finalise`, { method: "POST" });
      toast.success("Finalised");
      load();
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }

  if (error) {
    return (
      <main className="flex min-h-dvh items-center justify-center p-6 text-center">
        <div>
          <p className="font-medium">{error}</p>
          <p className="mt-1 text-sm text-muted-foreground">Ask for a new preview link.</p>
        </div>
      </main>
    );
  }
  if (!info) {
    return <main className="flex min-h-dvh items-center justify-center text-sm text-muted-foreground">Rendering…</main>;
  }

  const pdfHref = `/api/public/p/${token}/pdf`;
  return (
    <main className="min-h-dvh bg-muted/40 pb-28">
      <header className="sticky top-0 z-10 border-b bg-background/95 backdrop-blur">
        <div className="mx-auto flex max-w-3xl items-center gap-3 px-4 py-3">
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-2">
              <h1 className="truncate text-sm font-semibold">{info.title}</h1>
              <StatusBadge status={info.status} />
            </div>
            <p className="truncate text-xs text-muted-foreground">
              {info.number}
              {info.customer_name && ` · ${info.customer_name}`}
              {info.total && ` · ${money(info.total, info.currency)}`}
            </p>
          </div>
          <Button variant="ghost" size="icon" onClick={() => void load()} aria-label="Refresh preview" title="Refresh">
            <RefreshCw />
          </Button>
        </div>
      </header>

      <div className="mx-auto flex max-w-3xl flex-col gap-4 px-3 py-4">
        {Array.from({ length: info.pages }, (_, i) => (
          // eslint-disable-next-line @next/next/no-img-element
          <img
            key={`${info.version}-${i}`}
            src={`/api/public/p/${token}/pages/${i + 1}?v=${info.version}`}
            alt={`Page ${i + 1} of ${info.pages}`}
            className="w-full rounded-md bg-white shadow-sm ring-1 ring-black/5"
          />
        ))}
      </div>

      <div className="fixed inset-x-0 bottom-0 border-t bg-background pb-[env(safe-area-inset-bottom)]">
        <div className="mx-auto flex max-w-3xl gap-2 px-4 py-3">
          <a href={pdfHref} className={cn(buttonVariants({ variant: "outline", size: "lg" }), "h-11 flex-1")}>
            <Download /> {info.status === "draft" ? "Draft PDF" : "Download PDF"}
          </a>
          {me && info.status === "draft" && info.kind !== "template" && (
            <Button size="lg" className="h-11 flex-1" onClick={finalise} disabled={busy}>
              {busy ? "Finalising…" : "Finalise"}
            </Button>
          )}
          {me && info.kind !== "template" && info.status !== "draft" && (
            <Link href={`/${info.kind}s/${info.id}`} className={cn(buttonVariants({ size: "lg" }), "h-11 flex-1")}>
              Open in Desk
            </Link>
          )}
        </div>
      </div>
      {dialog}
    </main>
  );
}
