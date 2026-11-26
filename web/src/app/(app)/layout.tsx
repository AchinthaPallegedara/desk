import { Suspense } from "react";
import { AppShell } from "@/components/app-shell";

export default function AppLayout({ children }: LayoutProps<"/">) {
  return (
    <Suspense fallback={<div className="flex flex-1 items-center justify-center text-sm text-muted-foreground">Loading…</div>}>
      <AppShell>{children}</AppShell>
    </Suspense>
  );
}
