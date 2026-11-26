"use client";

import { useParams } from "next/navigation";
import { useState } from "react";
import { toast } from "sonner";
import { ExternalLink, RotateCcw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Badge } from "@/components/ui/badge";
import { PageHeader, Section, useConfirm } from "@/components/common";
import { useApi } from "@/hooks/use-api";
import { api, errorMessage, type Links, type TemplateVersion, type TemplateView } from "@/lib/api";
import { relative } from "@/lib/format";

export default function TemplatePage() {
  const { id } = useParams<{ id: string }>();
  const { data: t, setData } = useApi<TemplateView>(`/api/templates/${id}`);
  const { data: versions, reload: reloadVersions } = useApi<TemplateVersion[]>(`/api/templates/${id}/versions`);
  if (!t) return <p className="text-sm text-muted-foreground">Loading…</p>;
  return (
    <TemplateEditor
      key={`${t.current_version}-${t.updated_at}`}
      id={id}
      t={t}
      setData={setData}
      versions={versions}
      reloadVersions={reloadVersions}
    />
  );
}

function TemplateEditor({
  id,
  t,
  setData,
  versions,
  reloadVersions,
}: {
  id: string;
  t: TemplateView;
  setData: (t: TemplateView) => void;
  versions?: TemplateVersion[];
  reloadVersions: () => void;
}) {
  const [source, setSource] = useState(t.typst_source ?? "");
  const [name, setName] = useState(t.name);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const { confirm, dialog } = useConfirm();
  const dirty = source !== (t.typst_source ?? "") || name !== t.name;

  async function save() {
    setBusy(true);
    setError(null);
    try {
      const body: Record<string, unknown> = {};
      if (name !== t!.name) body.name = name;
      if (source !== t!.typst_source) body.typst_source = source;
      const nt = await api<TemplateView>(`/api/templates/${id}`, { method: "PATCH", body });
      setData(nt);
      void reloadVersions();
      toast.success(body.typst_source ? `Saved as version ${nt.current_version}` : "Saved");
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  }

  async function preview(versionId?: string) {
    const w = window.open("", "_blank");
    try {
      const l = await api<Links>(`/api/templates/${id}/preview${versionId ? `?version_id=${versionId}` : ""}`, { method: "POST" });
      if (w) w.location.href = l.preview_url;
    } catch (e) {
      w?.close();
      toast.error(errorMessage(e));
    }
  }

  async function makeDefault() {
    try {
      setData(await api<TemplateView>(`/api/templates/${id}`, { method: "PATCH", body: { is_default: true } }));
      toast.success("Now the default");
    } catch (e) {
      toast.error(errorMessage(e));
    }
  }

  async function restore(v: TemplateVersion) {
    const ok = await confirm({
      title: `Restore version ${v.version}?`,
      description: "It is copied into a new version and becomes current. Draft documents will use it.",
      action: "Restore",
    });
    if (!ok) return;
    try {
      setData(await api<TemplateView>(`/api/templates/${id}/versions/${v.id}/restore`, { method: "POST" }));
      void reloadVersions();
      toast.success("Restored");
    } catch (e) {
      toast.error(errorMessage(e));
    }
  }

  return (
    <>
      <PageHeader
        title={
          <span className="flex items-center gap-2">
            {t.name} {t.is_default && <Badge variant="secondary">Default</Badge>}
          </span>
        }
        description={`${t.doc_type === "estimate" ? "Estimate" : "Invoice"} template · version ${t.current_version}`}
        actions={
          <>
            {!t.is_default && (
              <Button variant="ghost" onClick={makeDefault}>
                Make default
              </Button>
            )}
            <Button variant="outline" onClick={() => preview()}>
              <ExternalLink /> Preview
            </Button>
            <Button onClick={save} disabled={!dirty || busy}>
              {busy ? "Compiling…" : "Save version"}
            </Button>
          </>
        }
      />
      <div className="grid gap-4 lg:grid-cols-[1fr_280px]">
        <div className="flex min-w-0 flex-col gap-3">
          <Input value={name} onChange={(e) => setName(e.target.value)} aria-label="Template name" />
          {error && (
            <pre className="overflow-x-auto rounded-lg border border-destructive/30 bg-destructive/5 p-3 text-xs whitespace-pre-wrap text-destructive">
              {error}
            </pre>
          )}
          <Textarea
            value={source}
            onChange={(e) => setSource(e.target.value)}
            spellCheck={false}
            className="min-h-[60vh] font-mono text-xs leading-relaxed"
          />
          <p className="text-xs text-muted-foreground">
            Each save is compiled against sample data first; broken templates are rejected with Typst&rsquo;s error.
          </p>
        </div>
        <Section title="Versions">
          <ul className="flex flex-col gap-1 text-sm">
            {versions?.map((v) => (
              <li key={v.id} className="flex items-center gap-2">
                <span className="w-8 font-medium">v{v.version}</span>
                <span className="flex-1 truncate text-xs text-muted-foreground">
                  {v.created_via} · {relative(v.created_at)}
                </span>
                <Button variant="ghost" size="icon-xs" onClick={() => preview(v.id)} aria-label={`Preview version ${v.version}`} title="Preview">
                  <ExternalLink />
                </Button>
                {v.version !== t.current_version && (
                  <Button variant="ghost" size="icon-xs" onClick={() => restore(v)} aria-label={`Restore version ${v.version}`} title="Restore">
                    <RotateCcw />
                  </Button>
                )}
              </li>
            ))}
          </ul>
        </Section>
      </div>
      {dialog}
    </>
  );
}
