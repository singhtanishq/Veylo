"use client";

// Contacts: accepted contacts, incoming/outgoing requests, blocked users.

import * as React from "react";
import { useRouter } from "next/navigation";
import { UserX, MessageSquare, Check, X as XIcon, Ban } from "lucide-react";
import { toast } from "sonner";
import { api } from "@/lib/api";
import { Avatar } from "@/components/ui/avatar";
import { Button } from "@/components/ui/button";
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/overlays";
import {
  useContacts,
  useContactRequests,
  useBlocks,
  useConversationMutation,
} from "@/hooks/use-queries";
import { lastSeenLabel, formatRelative } from "@/lib/utils";
import type { ContactUser, ContactRequest } from "@/lib/types";

export default function ContactsPage() {
  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-y-auto">
      <div className="mx-auto w-full max-w-2xl px-6 py-8">
        <h1 className="text-xl font-semibold tracking-tight" style={{ fontFamily: "var(--font-sora)" }}>
          Contacts
        </h1>
        <p className="mt-1 text-sm text-muted">The people you talk to, requests in flight, and blocked accounts.</p>

        <Tabs defaultValue="contacts" className="mt-6">
          <TabsList>
            <TabsTrigger value="contacts">Contacts</TabsTrigger>
            <TabsTrigger value="requests">Requests</TabsTrigger>
            <TabsTrigger value="blocked">Blocked</TabsTrigger>
          </TabsList>

          <TabsContent value="contacts">
            <ContactsTab />
          </TabsContent>
          <TabsContent value="requests">
            <RequestsTab />
          </TabsContent>
          <TabsContent value="blocked">
            <BlockedTab />
          </TabsContent>
        </Tabs>
      </div>
    </div>
  );
}

function ContactsTab() {
  const { data: contacts, isLoading } = useContacts();
  const removeContact = useConversationMutation(
    (vars: { userId: string }) => api.removeContact(vars.userId),
    { invalidate: ["contacts"] },
  );
  const router = useRouter();

  if (isLoading) return <SkeletonList />;
  if (!contacts || contacts.length === 0) {
    return (
      <EmptyState
        icon={<UserX size={26} className="text-muted/50" />}
        title="No contacts yet"
        body="Send contact requests from Discover — accepted requests appear here."
      />
    );
  }
  return (
    <ul className="mt-3 space-y-2">
      {contacts.map((u: ContactUser) => (
        <li key={u.id} className="flex items-center gap-4 rounded-2xl bg-surface p-4 shadow-sm">
          <Avatar src={u.avatar_url} name={u.display_name} userId={u.id} size={44} online={u.online} showPresence />
          <div className="min-w-0 flex-1">
            <p className="truncate text-[14px] font-semibold">{u.display_name}</p>
            <p className="truncate text-xs text-muted">@{u.username}</p>
            {!u.online && u.last_seen_at && (
              <p className="text-[11px] text-muted/70">{lastSeenLabel(u.last_seen_at)}</p>
            )}
          </div>
          <div className="flex shrink-0 items-center gap-2">
            <Button
              size="sm"
              onClick={async () => {
                const conv = await api.directConversation(u.id);
                router.push(`/?c=${conv.id}`);
              }}
            >
              <MessageSquare size={14} /> Message
            </Button>
            <Button
              size="sm"
              variant="ghost"
              onClick={async () => {
                try {
                  await removeContact.mutateAsync({ userId: u.id });
                  toast("Contact removed");
                } catch (err) {
                  toast.error(err instanceof Error ? err.message : "Failed");
                }
              }}
            >
              Remove
            </Button>
          </div>
        </li>
      ))}
    </ul>
  );
}

function RequestsTab() {
  const incoming = useContactRequests("incoming");
  const outgoing = useContactRequests("outgoing");
  const accept = useConversationMutation((id: string) => api.acceptContactRequest(id), {
    invalidate: ["contactRequests", "contacts", "notifications"],
  });
  const decline = useConversationMutation((id: string) => api.declineContactRequest(id), {
    invalidate: ["contactRequests", "notifications"],
  });

  const incomingList = incoming.data ?? [];
  const outgoingList = outgoing.data ?? [];

  if (incomingList.length === 0 && outgoingList.length === 0) {
    return (
      <EmptyState
        icon={<Check size={26} className="text-muted/50" />}
        title="No pending requests"
        body="Contact requests you send or receive show up here."
      />
    );
  }

  return (
    <div className="mt-3 space-y-6">
      {incomingList.length > 0 && (
        <section>
          <p className="mb-2 text-[11px] font-semibold uppercase tracking-wide text-muted">Incoming</p>
          <ul className="space-y-2">
            {incomingList.map((r: ContactRequest) => (
              <li key={r.id} className="flex items-center gap-4 rounded-2xl bg-surface p-4 shadow-sm">
                <Avatar src={r.user.avatar_url} name={r.user.display_name} userId={r.user.id} size={44} />
                <div className="min-w-0 flex-1">
                  <p className="truncate text-[14px] font-semibold">{r.user.display_name}</p>
                  <p className="truncate text-xs text-muted">
                    @{r.user.username} · {formatRelative(r.created_at)}
                  </p>
                  {r.message && <p className="mt-0.5 truncate text-xs italic text-muted/80">“{r.message}”</p>}
                </div>
                <div className="flex shrink-0 gap-2">
                  <Button
                    size="sm"
                    onClick={async () => {
                      await accept.mutateAsync(r.id);
                      toast.success(`Added ${r.user.display_name}`);
                    }}
                  >
                    <Check size={14} /> Accept
                  </Button>
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={async () => {
                      await decline.mutateAsync(r.id);
                      toast("Request declined");
                    }}
                  >
                    <XIcon size={14} /> Decline
                  </Button>
                </div>
              </li>
            ))}
          </ul>
        </section>
      )}
      {outgoingList.length > 0 && (
        <section>
          <p className="mb-2 text-[11px] font-semibold uppercase tracking-wide text-muted">Sent</p>
          <ul className="space-y-2">
            {outgoingList.map((r: ContactRequest) => (
              <li key={r.id} className="flex items-center gap-4 rounded-2xl bg-surface p-4 shadow-sm opacity-80">
                <Avatar src={r.user.avatar_url} name={r.user.display_name} userId={r.user.id} size={40} />
                <div className="min-w-0 flex-1">
                  <p className="truncate text-[13px] font-medium">{r.user.display_name}</p>
                  <p className="truncate text-[11px] text-muted">
                    Waiting for @{r.user.username} · sent {formatRelative(r.created_at)}
                  </p>
                </div>
                <Button
                  size="sm"
                  variant="ghost"
                  onClick={async () => {
                    await decline.mutateAsync(r.id);
                    toast("Request withdrawn");
                  }}
                >
                  Withdraw
                </Button>
              </li>
            ))}
          </ul>
        </section>
      )}
    </div>
  );
}

function BlockedTab() {
  const { data: blocks } = useBlocks();
  const unblock = useConversationMutation((vars: { userId: string }) => api.unblock(vars.userId), {
    invalidate: ["blocks"],
  });

  if (!blocks || blocks.length === 0) {
    return (
      <EmptyState
        icon={<Ban size={26} className="text-muted/50" />}
        title="Nobody is blocked"
        body="Blocked users can't message you, see your presence, or find you in search."
      />
    );
  }
  return (
    <ul className="mt-3 space-y-2">
      {blocks.map((u: ContactUser) => (
        <li key={u.id} className="flex items-center gap-4 rounded-2xl bg-surface p-4 shadow-sm">
          <Avatar src={u.avatar_url} name={u.display_name} userId={u.id} size={40} />
          <div className="min-w-0 flex-1">
            <p className="truncate text-[13.5px] font-medium">{u.display_name}</p>
            <p className="truncate text-xs text-muted">@{u.username}</p>
          </div>
          <Button
            size="sm"
            variant="outline"
            onClick={async () => {
              await unblock.mutateAsync({ userId: u.id });
              toast(`${u.display_name} unblocked`);
            }}
          >
            Unblock
          </Button>
        </li>
      ))}
    </ul>
  );
}

function SkeletonList() {
  return (
    <div className="mt-3 space-y-2">
      {Array.from({ length: 3 }).map((_, i) => (
        <div key={i} className="flex items-center gap-4 rounded-2xl bg-surface p-4">
          <div className="vy-skeleton h-11 w-11 rounded-full" />
          <div className="flex-1 space-y-2">
            <div className="vy-skeleton h-3.5 w-36" />
            <div className="vy-skeleton h-3 w-20" />
          </div>
        </div>
      ))}
    </div>
  );
}

function EmptyState({ icon, title, body }: { icon: React.ReactNode; title: string; body: string }) {
  return (
    <div className="flex flex-col items-center gap-2 rounded-2xl bg-surface px-6 py-14 text-center shadow-sm">
      {icon}
      <p className="text-sm font-medium">{title}</p>
      <p className="max-w-xs text-xs text-muted leading-relaxed">{body}</p>
    </div>
  );
}
