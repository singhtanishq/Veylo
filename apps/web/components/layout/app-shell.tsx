"use client";

// Veylo workspace shell: icon navigation rail + content area.

import * as React from "react";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useQueryClient } from "@tanstack/react-query";
import {
  MessagesSquare,
  Compass,
  Users,
  Settings,
  LogOut,
  Moon,
  Sun,
  Monitor,
} from "lucide-react";
import { toast } from "sonner";
import { api } from "@/lib/api";
import { realtime } from "@/lib/ws";
import { useMe } from "@/hooks/use-queries";
import { useChatStore } from "@/stores/chat";
import { Avatar, Tooltip } from "@/components/ui/avatar";
import {
  Dropdown,
  DropdownTrigger,
  DropdownContent,
  DropdownItem,
  DropdownSeparator,
  DropdownLabel,
} from "@/components/ui/overlays";
import { cn } from "@/lib/utils";

const NAV = [
  { href: "/", label: "Messages", icon: MessagesSquare, match: (p: string) => p === "/" },
  { href: "/discover", label: "Discover", icon: Compass, match: (p: string) => p.startsWith("/discover") },
  { href: "/contacts", label: "Contacts", icon: Users, match: (p: string) => p.startsWith("/contacts") },
  { href: "/settings", label: "Settings", icon: Settings, match: (p: string) => p.startsWith("/settings") },
];

export function AppShell({ children }: { children: React.ReactNode }) {
  const pathname = usePathname();
  const { data: me } = useMe();
  const connection = useChatStore((s) => s.connection);

  React.useEffect(() => {
    realtime.connect();
  }, []);

  return (
    <div className="flex h-screen overflow-hidden bg-background">
      <nav className="flex w-[64px] shrink-0 flex-col items-center border-r border-border bg-surface py-4">
        <Link href="/" className="mb-6" aria-label="Veylo home">
          <VeyloMark />
        </Link>
        <div className="flex flex-1 flex-col items-center gap-1.5">
          {NAV.map((item) => {
            const active = item.match(pathname);
            return (
              <Tooltip key={item.href} content={item.label} side="right">
                <Link
                  href={item.href}
                  className={cn(
                    "flex h-11 w-11 items-center justify-center rounded-xl transition-all",
                    active
                      ? "bg-primary-soft text-primary"
                      : "text-muted hover:text-foreground hover:bg-surface-2",
                  )}
                  aria-label={item.label}
                  aria-current={active ? "page" : undefined}
                >
                  <item.icon size={20} strokeWidth={active ? 2.2 : 1.8} />
                </Link>
              </Tooltip>
            );
          })}
        </div>

        <div className="flex flex-col items-center gap-3">
          <ConnectionDot connection={connection} />
          {me && <ProfileMenu me={me} />}
        </div>
      </nav>

      <main className="flex-1 min-w-0 flex flex-col">{children}</main>
    </div>
  );
}

function ConnectionDot({ connection }: { connection: string }) {
  const color =
    connection === "connected" ? "bg-success" : connection === "connecting" ? "bg-warning" : "bg-muted/60";
  const label =
    connection === "connected"
      ? "Realtime connected"
      : connection === "connecting"
        ? "Connecting…"
        : connection === "disconnected"
          ? "Reconnecting…"
          : "Offline";
  return (
    <Tooltip content={label} side="left">
      <span className="flex h-6 w-6 items-center justify-center" aria-label={label}>
        <span className={cn("h-2 w-2 rounded-full transition-colors", color)} />
      </span>
    </Tooltip>
  );
}

function ProfileMenu({
  me,
}: {
  me: { id: string; username: string; display_name: string; avatar_url: string | null };
}) {
  const router = useRouter();
  const queryClient = useQueryClient();
  const clearOptimistic = useChatStore((s) => s.clearOptimistic);

  async function logout() {
    try {
      await api.logout();
    } catch {
      // Best effort — clear client state regardless.
    }
    realtime.dispose();
    clearOptimistic("");
    queryClient.clear();
    toast("Signed out");
    router.push("/login");
    router.refresh();
  }

  return (
    <Dropdown>
      <DropdownTrigger asChild>
        <button className="rounded-full focus-visible:outline-2 focus-visible:outline-ring" aria-label="Account menu">
          <Avatar src={me.avatar_url} name={me.display_name} userId={me.id} size={36} />
        </button>
      </DropdownTrigger>
      <DropdownContent align="end" className="min-w-56">
        <div className="px-3 py-2">
          <p className="truncate text-sm font-semibold">{me.display_name}</p>
          <p className="truncate text-xs text-muted">@{me.username}</p>
        </div>
        <DropdownSeparator />
        <DropdownLabel>Theme</DropdownLabel>
        <ThemeItems />
        <DropdownSeparator />
        <DropdownItem onSelect={() => router.push("/settings")}>
          <Settings size={15} /> Settings
        </DropdownItem>
        <DropdownItem danger onSelect={logout}>
          <LogOut size={15} /> Sign out
        </DropdownItem>
      </DropdownContent>
    </Dropdown>
  );
}

function ThemeItems() {
  const [theme, setTheme] = React.useState<string>("system");

  React.useEffect(() => {
    setTheme(localStorage.getItem("veylo-theme") ?? "system");
  }, []);

  function apply(t: string) {
    localStorage.setItem("veylo-theme", t);
    const dark = t === "dark" || (t === "system" && window.matchMedia("(prefers-color-scheme: dark)").matches);
    document.documentElement.classList.toggle("dark", dark);
    setTheme(t);
  }

  return (
    <div className="flex gap-1 px-2 pb-1.5">
      {[
        { key: "light", icon: Sun, label: "Light" },
        { key: "dark", icon: Moon, label: "Dark" },
        { key: "system", icon: Monitor, label: "System" },
      ].map(({ key, icon: Icon, label }) => (
        <button
          key={key}
          onClick={() => apply(key)}
          className={cn(
            "flex flex-1 flex-col items-center gap-1 rounded-lg py-2 text-[10px] font-medium transition-colors",
            theme === key ? "bg-primary-soft text-primary" : "text-muted hover:bg-surface-2",
          )}
        >
          <Icon size={15} />
          {label}
        </button>
      ))}
    </div>
  );
}

function VeyloMark() {
  return (
    <svg width="34" height="34" viewBox="0 0 40 40" fill="none" aria-hidden>
      <rect width="40" height="40" rx="11" fill="#5b4fc4" />
      <path d="M11 13.5L20 28.5L29 13.5" stroke="white" strokeWidth="3.2" strokeLinecap="round" strokeLinejoin="round" />
      <circle cx="20" cy="12" r="2.6" fill="#e0b34d" />
    </svg>
  );
}
