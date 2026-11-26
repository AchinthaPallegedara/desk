"use client";

import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { createContext, useContext, useEffect, useState } from "react";
import { FileText, Receipt, Users, LayoutTemplate, Settings, LogOut } from "lucide-react";
import { Button } from "@/components/ui/button";
import { api, type Me } from "@/lib/api";
import { cn } from "@/lib/utils";

const nav = [
  { href: "/estimates", label: "Estimates", icon: FileText },
  { href: "/invoices", label: "Invoices", icon: Receipt },
  { href: "/customers", label: "Customers", icon: Users },
  { href: "/templates", label: "Templates", icon: LayoutTemplate },
  { href: "/settings", label: "Settings", icon: Settings },
];

const MeContext = createContext<Me | null>(null);
export const useMe = () => useContext(MeContext);

export function AppShell({ children }: { children: React.ReactNode }) {
  const pathname = usePathname();
  const router = useRouter();
  const [me, setMe] = useState<Me | null>(null);

  useEffect(() => {
    api<Me>("/api/auth/me")
      .then(setMe)
      .catch(() => {}); // api() redirects to /login on 401
  }, []);

  async function logout() {
    await api("/api/auth/logout", { method: "POST" });
    router.replace("/login");
  }

  if (!me) {
    return <div className="flex flex-1 items-center justify-center text-sm text-muted-foreground">Loading…</div>;
  }

  return (
    <MeContext.Provider value={me}>
      <div className="flex min-h-dvh flex-col">
        <header className="sticky top-0 z-30 border-b bg-background/95 backdrop-blur supports-backdrop-filter:bg-background/80">
          <div className="mx-auto flex h-14 w-full max-w-6xl items-center gap-6 px-4">
            <Link href="/estimates" className="flex items-baseline gap-1.5 font-semibold tracking-tight">
              <span className="text-primary">Claviq</span>
              <span className="text-muted-foreground font-normal">Desk</span>
            </Link>
            <nav className="hidden items-center gap-1 md:flex">
              {nav.map((n) => (
                <Link
                  key={n.href}
                  href={n.href}
                  className={cn(
                    "rounded-md px-3 py-1.5 text-sm text-muted-foreground transition-colors hover:text-foreground",
                    pathname.startsWith(n.href) && "bg-muted text-foreground",
                  )}
                >
                  {n.label}
                </Link>
              ))}
            </nav>
            <div className="ml-auto flex items-center gap-3">
              <span className="hidden text-sm text-muted-foreground sm:inline">{me.tenant.name}</span>
              <Button variant="ghost" size="icon" onClick={logout} aria-label="Sign out" title="Sign out">
                <LogOut />
              </Button>
            </div>
          </div>
        </header>

        <main className="mx-auto w-full max-w-6xl flex-1 px-4 pt-6 pb-24 md:pb-10">{children}</main>

        {/* Mobile bottom navigation */}
        <nav className="fixed inset-x-0 bottom-0 z-30 border-t bg-background pb-[env(safe-area-inset-bottom)] md:hidden">
          <div className="grid grid-cols-5">
            {nav.map((n) => {
              const active = pathname.startsWith(n.href);
              return (
                <Link
                  key={n.href}
                  href={n.href}
                  className={cn(
                    "flex flex-col items-center gap-1 py-2.5 text-[11px] text-muted-foreground",
                    active && "text-primary",
                  )}
                >
                  <n.icon className="size-5" />
                  {n.label}
                </Link>
              );
            })}
          </div>
        </nav>
      </div>
    </MeContext.Provider>
  );
}
