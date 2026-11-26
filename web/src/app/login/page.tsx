"use client";

import { Suspense, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Field } from "@/components/common";
import { api, errorMessage } from "@/lib/api";

// Only allow same-site relative paths after login (no open redirects).
function safeNext(next: string | null): string {
  if (!next || !next.startsWith("/") || next.startsWith("//") || next.startsWith("/\\")) return "/estimates";
  return next;
}

function LoginForm() {
  const router = useRouter();
  const params = useSearchParams();
  const next = safeNext(params.get("next"));
  const connecting = next.startsWith("/oauth/");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await api("/api/auth/login", { method: "POST", body: { email, password } });
      // OAuth consent is served by the Go backend: use a full navigation.
      if (connecting) window.location.href = next;
      else router.replace(next);
    } catch (err) {
      setError(errorMessage(err));
      setBusy(false);
    }
  }

  return (
    <form onSubmit={submit} className="w-full max-w-sm rounded-xl border bg-card p-6">
      <div className="mb-6">
        <div className="flex items-baseline gap-1.5 text-lg font-semibold tracking-tight">
          <span className="text-primary">Claviq</span>
          <span className="font-normal text-muted-foreground">Desk</span>
        </div>
        <p className="mt-1 text-sm text-muted-foreground">
          {connecting ? "Sign in to connect your AI assistant." : "Sign in to your business."}
        </p>
      </div>
      <div className="flex flex-col gap-4">
        <Field label="Email">
          <Input
            type="email"
            autoComplete="username"
            required
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            className="h-10"
          />
        </Field>
        <Field label="Password">
          <Input
            type="password"
            autoComplete="current-password"
            required
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            className="h-10"
          />
        </Field>
        {error && <p className="text-sm text-destructive">{error}</p>}
        <Button type="submit" size="lg" className="h-10" disabled={busy}>
          {busy ? "Signing in…" : "Sign in"}
        </Button>
      </div>
    </form>
  );
}

export default function LoginPage() {
  return (
    <main className="flex min-h-dvh items-center justify-center p-4">
      <Suspense>
        <LoginForm />
      </Suspense>
    </main>
  );
}
