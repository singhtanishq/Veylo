"use client";

// Conversation list with search, filters and realtime unread badges.

import * as React from "react";
import {
  Search,
  Pin,
  VolumeX,
  Archive,
  MessageSquare,
  Users as UsersIcon,
  AtSign,
  Inbox,
} from "lucide-react";
import { Avatar } from "@/components/ui/avatar";
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/overlays";
import { Skeleton } from "@/components/ui/avatar";
import { useConversations } from "@/hooks/use-queries";
import { useChatStore } from "@/stores/chat";
import { cn, formatRelative } from "@/lib/utils";
import type { Conversation } from "@/lib/types";

type Filter = "all" | "unread" | "groups" | "archived";

// Stable empty-array reference: zustand selectors must not allocate per call.
const EMPTY_LIST: string[] = [];

export function ConversationList({
  activeId,
  onSelect,
}: {
  activeId: string | null;
  onSelect: (id: string) => void;
}) {
  const [query, setQuery] = React.useState("");
  const [filter, setFilter] = React.useState<Filter>("all");
  const { data: conversations, isLoading } = useConversations();

  const filtered = React.useMemo(() => {
    let list = conversations ?? [];
    if (filter === "unread") list = list.filter((c) => c.unread_count > 0);
    if (filter === "groups") list = list.filter((c) => c.type === "group");
    if (filter === "archived") list = list.filter((c) => c.is_archived);
    if (query.trim()) {
      const q = query.trim().toLowerCase();
      list = list.filter(
        (c) =>
          (c.title ?? "").toLowerCase().includes(q) ||
          (c.other_user?.username ?? "").toLowerCase().includes(q) ||
          (c.last_message?.preview ?? "").toLowerCase().includes(q),
      );
    }
    return [...list].sort((a, b) => {
      if (a.is_pinned !== b.is_pinned) return a.is_pinned ? -1 : 1;
      const ta = a.last_message_at ? Date.parse(a.last_message_at) : 0;
      const tb = b.last_message_at ? Date.parse(b.last_message_at) : 0;
      return tb - ta;
    });
  }, [conversations, filter, query]);

  const unreadTotal = (conversations ?? []).reduce((acc, c) => acc + c.unread_count, 0);

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      {/* Search */}
      <div className="px-3 pb-2">
        <div className="relative">
          <Search size={15} className="absolute left-3 top-1/2 -translate-y-1/2 text-muted" />
          <input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search conversations"
            aria-label="Search conversations"
            className="h-9 w-full rounded-xl bg-surface-2 pl-9 pr-3 text-sm placeholder:text-muted/70 focus:outline-none focus:ring-2 focus:ring-ring/40"
          />
        </div>
      </div>

      {/* Filter tabs */}
      <Tabs value={filter} onValueChange={(v) => setFilter(v as Filter)} className="px-3">
        <TabsList className="w-full justify-start overflow-x-auto">
          <TabsTrigger value="all">All</TabsTrigger>
          <TabsTrigger value="unread">
            Unread{unreadTotal > 0 && <span className="ml-1 rounded-full bg-primary px-1.5 text-[10px] text-primary-foreground">{unreadTotal}</span>}
          </TabsTrigger>
          <TabsTrigger value="groups">Groups</TabsTrigger>
          <TabsTrigger value="archived">Archived</TabsTrigger>
        </TabsList>
      </Tabs>

      <div className="min-h-0 flex-1 overflow-y-auto px-2 py-2">
        {isLoading ? (
          <div className="space-y-2 px-1">
            {Array.from({ length: 6 }).map((_, i) => (
              <div key={i} className="flex items-center gap-3">
                <Skeleton className="h-11 w-11 rounded-full" />
                <div className="flex-1 space-y-1.5">
                  <Skeleton className="h-3.5 w-2/3" />
                  <Skeleton className="h-3 w-1/2" />
                </div>
              </div>
            ))}
          </div>
        ) : filtered.length === 0 ? (
          <EmptyList filter={filter} />
        ) : (
          <ul className="space-y-0.5">
            {filtered.map((c) => (
              <ConversationItem
                key={c.id}
                conversation={c}
                active={c.id === activeId}
                onSelect={() => onSelect(c.id)}
              />
            ))}
          </ul>
        )}
      </div>
    </div>
  );
}

function EmptyList({ filter }: { filter: Filter }) {
  return (
    <div className="flex flex-col items-center justify-center gap-2 px-6 py-14 text-center">
      <Inbox size={26} className="text-muted/60" />
      <p className="text-sm font-medium text-foreground/80">
        {filter === "unread" ? "You're all caught up" : "No conversations here yet"}
      </p>
      <p className="text-xs text-muted">
        {filter === "archived"
          ? "Archived conversations appear here."
          : "Use the + button to find someone and start chatting."}
      </p>
    </div>
  );
}

function ConversationItem({
  conversation: c,
  active,
  onSelect,
}: {
  conversation: Conversation;
  active: boolean;
  onSelect: () => void;
}) {
  const typing = useChatStore((s) => s.typing[c.id] ?? EMPTY_LIST);
  const presence = useChatStore((s) => s.presence);

  const otherOnline = c.other_user ? (presence[c.other_user.id] ?? c.other_user.online) : false;

  return (
    <li>
      <button
        onClick={onSelect}
        className={cn(
          "group flex w-full items-center gap-3 rounded-xl px-2.5 py-2.5 text-left transition-colors",
          active ? "bg-primary-soft" : "hover:bg-surface-2",
        )}
      >
        {c.type === "group" ? (
          <div className="relative">
            {c.avatar_url ? (
              <Avatar src={c.avatar_url} name={c.title ?? "Group"} userId={c.id} size={44} />
            ) : (
              <div className="flex h-11 w-11 items-center justify-center rounded-full bg-primary-soft text-primary">
                <UsersIcon size={20} />
              </div>
            )}
          </div>
        ) : (
          <Avatar
            src={c.other_user?.avatar_url}
            name={c.other_user?.display_name ?? c.title ?? "?"}
            userId={c.other_user?.id ?? c.id}
            size={44}
            online={otherOnline}
            showPresence
          />
        )}

        <div className="min-w-0 flex-1">
          <div className="flex items-center justify-between gap-2">
            <span className={cn("truncate text-[14px]", c.unread_count > 0 ? "font-semibold" : "font-medium")}>
              {c.type === "group" ? c.title : c.other_user?.display_name ?? c.title}
            </span>
            <span className="shrink-0 text-[11px] text-muted">
              {c.last_message_at ? formatRelative(c.last_message_at) : ""}
            </span>
          </div>
          <div className="mt-0.5 flex items-center justify-between gap-2">
            <span className={cn("truncate text-[13px]", c.unread_count > 0 ? "text-foreground/80" : "text-muted")}>
              {typing.length > 0 ? (
                <span className="text-primary">typing…</span>
              ) : c.last_message ? (
                <>
                  {c.type === "group" && c.last_message.sender_id && c.last_message.type !== "system" && (
                    <span className="text-muted">{c.last_message.sender_name.split(" ")[0]}: </span>
                  )}
                  {c.last_message.preview || "…"}
                </>
              ) : (
                <span className="italic text-muted/70">No messages yet</span>
              )}
            </span>
            <span className="flex shrink-0 items-center gap-1.5">
              {c.is_muted && <VolumeX size={13} className="text-muted/70" />}
              {c.is_pinned && <Pin size={13} className="text-muted/70" />}
              {c.is_archived && <Archive size={13} className="text-muted/70" />}
              {c.unread_count > 0 && (
                <span
                  className={cn(
                    "min-w-5 rounded-full px-1.5 py-0.5 text-center text-[11px] font-semibold leading-none",
                    c.is_muted ? "bg-surface-3 text-muted" : "bg-primary text-primary-foreground",
                  )}
                >
                  {c.unread_count > 99 ? "99+" : c.unread_count}
                </span>
              )}
            </span>
          </div>
        </div>
      </button>
    </li>
  );
}

// Re-exported icon keeps the import list tidy for future filters.
export { MessageSquare, AtSign };
