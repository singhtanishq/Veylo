"use client";

// Global providers: TanStack Query + the realtime WebSocket wired to query
// invalidation, presence, typing, receipts and notifications.

import * as React from "react";
import { QueryClient, QueryClientProvider, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api } from "@/lib/api";
import { realtime } from "@/lib/ws";
import type { WSEnvelope } from "@/lib/types";
import { TooltipProvider } from "@/components/ui/avatar";
import { useChatStore } from "@/stores/chat";
import type { Message } from "@/lib/types";

const RealtimeBridge = () => {
  const queryClient = useQueryClient();
  const setConnection = useChatStore((s) => s.setConnection);
  const setPresence = useChatStore((s) => s.setPresence);
  const markTyping = useChatStore((s) => s.markTyping);

  React.useEffect(() => {
    realtime.setStateListener(setConnection);

    const handle = (env: WSEnvelope) => {
      switch (env.type) {
        case "__resync":
          // Reconnected: refresh server state (messages, receipts, unread).
          queryClient.invalidateQueries({ queryKey: ["conversations"] });
          queryClient.invalidateQueries({ queryKey: ["messages"] });
          queryClient.invalidateQueries({ queryKey: ["conversation"] });
          break;
        case "connection.ready":
          setPresenceReady(true);
          break;
        case "message.created": {
          const msg = env.data?.message as Message | undefined;
          if (msg && !msg.is_mine) {
            // Delivery ack: a recipient session received the event.
            realtime.send("receipt.deliver", {
              conversation_id: msg.conversation_id,
              max_seq: msg.seq,
            });
          }
          if (msg) {
            queryClient.setQueryData<{ pages: { messages: Message[] }[]; pageParams: unknown[] }>(
              ["messages", msg.conversation_id],
              (old) => {
                if (!old?.pages) return old;
                const pages = old.pages.map((page) => {
                  const without = page.messages.filter(
                    (m) =>
                      m.id !== msg.id &&
                      !(m.client_message_id && msg.client_message_id && m.client_message_id === msg.client_message_id),
                  );
                  return { ...page, messages: [...without, msg] };
                });
                return { ...old, pages };
              },
            );
            queryClient.invalidateQueries({ queryKey: ["conversations"] });
          }
          break;
        }
        case "message.updated": {
          const msg = env.data?.message as Message | undefined;
          if (msg) {
            queryClient.setQueryData<{ pages: { messages: Message[] }[]; pageParams: unknown[] }>(
              ["messages", msg.conversation_id],
              (old) => {
                if (!old?.pages) return old;
                return {
                  ...old,
                  pages: old.pages.map((page) => ({
                    ...page,
                    messages: page.messages.map((m) => (m.id === msg.id ? msg : m)),
                  })),
                };
              },
            );
            queryClient.invalidateQueries({ queryKey: ["conversations"] });
          }
          break;
        }
        case "message.deleted":
          queryClient.invalidateQueries({
            queryKey: ["messages", env.data?.conversation_id],
          });
          queryClient.invalidateQueries({ queryKey: ["conversations"] });
          break;
        case "receipt.delivered":
        case "receipt.read": {
          const convId = env.data?.conversation_id as string | undefined;
          if (convId) {
            queryClient.invalidateQueries({ queryKey: ["messages", convId] });
          }
          break;
        }
        case "typing.start":
          markTyping(env.data?.conversation_id, env.data?.user_id, true);
          break;
        case "typing.stop":
          markTyping(env.data?.conversation_id, env.data?.user_id, false);
          break;
        case "presence.online":
          setPresence(env.data?.user_id, true);
          break;
        case "presence.offline":
          setPresence(env.data?.user_id, false);
          break;
        case "conversation.created":
        case "conversation.updated":
        case "conversation.deleted":
        case "conversation.member_added":
        case "conversation.member_removed":
          queryClient.invalidateQueries({ queryKey: ["conversations"] });
          if (env.data?.conversation_id) {
            queryClient.invalidateQueries({ queryKey: ["conversation", env.data.conversation_id] });
          } else if (env.data?.conversation?.id) {
            queryClient.invalidateQueries({ queryKey: ["conversation", env.data.conversation.id] });
          }
          break;
        case "reaction.added":
        case "reaction.removed":
          if (env.data?.conversation_id) {
            queryClient.invalidateQueries({ queryKey: ["messages", env.data.conversation_id] });
          }
          break;
        case "message.failed":
        case "notification.created":
        case "connection.error":
          queryClient.invalidateQueries({ queryKey: ["notifications"] });
          if (env.type === "notification.created") {
            const n = env.data?.notification;
            if (n?.title && !n.conversation_id) {
              toast(n.title, { description: n.body ?? undefined });
            }
            queryClient.invalidateQueries({ queryKey: ["conversations"] });
          }
          if (env.type === "message.failed") {
            toast.error(env.data?.error?.message ?? "Message failed to send");
          }
          break;
        default:
          break;
      }
    };

    return realtime.on(handle);
  }, [queryClient, setConnection, setPresence, markTyping]);

  return null;
};

let presenceReady = false;
function setPresenceReady(v: boolean) {
  presenceReady = v;
}

export function Providers({ children }: { children: React.ReactNode }) {
  const [queryClient] = React.useState(
    () =>
      new QueryClient({
        defaultOptions: {
          queries: {
            staleTime: 15_000,
            retry: (failureCount, error) => {
              const status = (error as { status?: number })?.status ?? 0;
              if (status === 401 || status === 403 || status === 404) return false;
              return failureCount < 2;
            },
            refetchOnWindowFocus: true,
          },
        },
      }),
  );

  React.useEffect(() => {
    realtime.connect();
    return () => {
      // Keep the singleton alive across route changes; disposed on logout.
    };
  }, []);

  return (
    <QueryClientProvider client={queryClient}>
      <TooltipProvider>
        <RealtimeBridge />
        {children}
      </TooltipProvider>
    </QueryClientProvider>
  );
}
