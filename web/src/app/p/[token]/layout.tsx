import { Suspense } from "react";

export default function PreviewLayout({ children }: LayoutProps<"/p/[token]">) {
  return (
    <Suspense fallback={<main className="flex min-h-dvh items-center justify-center text-sm text-muted-foreground">Rendering…</main>}>
      {children}
    </Suspense>
  );
}
