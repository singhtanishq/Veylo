"use client";

// Notification bell: unread badge + dropdown feed with mark-as-read.

import * as React from "react";
import { useRouter } from "next/navigation";
import { Bell, CheckCheck, MessageSquare, AtSign, Users, UserPlus } from "lucide-react";
import { toast } from "sonner";
import { useNotifications, useConversationMutation } from "@/hooks/use-queries";
import { api } from "@/lib/api";
import { Avatar } from "@/components/ui/avatar";
import { Popover, PopoverTrigger, PopoverContent } from "@/components/ui/overlays";
import { cn, formatRelative } from "@/lib/utils";
import type { Notification } from "@/lib/types";

const ICONS: Record<string, React.ComponentType<{ size?: number; className?: string }>> = {
  new_message: MessageSquare,
  mention: AtSign,
  contact_request: UserPlus,
  contact_accepted: Users,
  group_invite: Users,
  system_alert: Bell,
};

export function NotificationBell() {
  const router = useRouter();
  const { data: notifications } = useNotifications();
  const [open, setOpen] = React.useState(false);
  const markRead = useConversationMutation(
    (vars: { ids?: string[]; all?: boolean }) => api.markNotificationsRead(vars.ids, vars.all),
    { invalidate: ["notifications"] },
  );

  const unread = (notifications ?? []).filter((n) => !n.read_at);

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <button
          className="relative flex h-11 w-11 items-center justify-center rounded-xl text-muted transition-colors hover:bg-surface-2 hover:text-foreground"
          aria-label={`Notifications${unread.length > 0 ? ` (${unread.length} unread)` : ""}`}
        >
          <Bell size={19} />
          {unread.length > 0 && (
            <span className="absolute right-1.5 top-1.5 flex h-4 min-w-4 items-center justify-center rounded-full bg-danger px-1 text-[10px] font-bold leading-none text-white">
              {unread.length > 9 ? "9+" : unread.length}
            </span>
          )}
        </button>
      </PopoverTrigger>
      <PopoverContent align="start" sideOffset={8} className="w-[340px] p-0">
        <div className="flex items-center justify-between border-b border-border px-4 py-3">
          <h3 className="text-[14px] font-semibold" style={{ fontFamily: "var(--font-sora)" }}>
            Notifications
          </h3>
          {unread.length > 0 && (
            <button
              onClick={async () => {
                await markRead.mutateAsync({ all: true });
                toast("All notifications marked as read");
              }}
              className="flex items-center gap-1 text-[11px] font-medium text-primary hover:underline"
            >
              <CheckCheck size={13} /> Mark all read
            </button>
          )}
        </div>
        <div className="max-h-[380px] overflow-y-auto">
          {(notifications ?? []).length === 0 ? (
            <p className="px-4 py-10 text-center text-xs text-muted">Nothing yet — you&apos;re all caught up.</p>
          ) : (
            <ul>
              {(notifications ?? []).slice(0, 12).map((n: Notification) => {
                const Icon = ICONS[n.type] ?? Bell;
                return (
                  <li key={n.id}>
                    <button
                      onClick={async () => {
                        if (!n.read_at) await markRead.mutateAsync({ ids: [n.id] });
                        if (n.conversation_id) {
                          router.push(`/?c=${n.conversation_id}`);
                          setOpen(false);
                        }
                      }}
                      className={cn(
                        "flex w-full items-start gap-3 px-4 py-3 text-left transition-colors hover:bg-surface-2",
                        !n.read_at && "bg-primary-soft/40",
                      )}
                    >
                      {n.actor ? (
                        <Avatar src={n.actor.avatar_url} name={n.actor.display_name} userId={n.actor.id} size={34} />
                      ) : (
                        <span className="flex h-[34px] w-[34px] items-center justify-center rounded-full bg-surface-2 text-muted">
                          <Icon size={15} />
                        </span>
                      )}
                      <span className="min-w-0 flex-1">
                        <span className={cn("block truncate text-[13px]", n.read_at ? "font-medium" : "font-semibold")}>
                          {n.title}
                        </span>
                        {n.body && <span className="block truncate text-[12px] text-muted">{n.body}</span>}
                        <span className="mt-0.5 block text-[10.5px] text-muted/70">{formatRelative(n.created_at)}</span>
                      </span>
                      {!n.read_at && <span className="mt-1.5 h-2 w-2 shrink-0 rounded-full bg-primary" />}
                    </button>
                  </li>
                );
              })}
            </ul>
          )}
        </div>
      </PopoverContent>
    </Popover>
  );
}
