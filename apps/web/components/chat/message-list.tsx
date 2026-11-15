"use client";

// Virtualized message history: day separators, grouped bubbles, receipts,
// unread divider, typing indicator, automatic older-page loading with scroll
// position preservation, and a jump-to-latest pill.

import * as React from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import { ArrowDown, Check, CheckCheck } from "lucide-react";
import { useChatStore } from "@/stores/chat";
import { useInfiniteMessages, flattenMessages } from "@/hooks/use-queries";
import { MessageBubble } from "@/components/chat/message-bubble";
import { formatDayLabel } from "@/lib/utils";
import type { Message } from "@/lib/types";

type Row =
  | { kind: "day"; key: string; label: string }
  | { kind: "msg"; key: string; message: Message; grouped: boolean }
  | { kind: "typing"; key: string };

const GROUP_WINDOW_MS = 4 * 60 * 1000;

// Stable references for zustand selectors (no per-call allocation).
const EMPTY_STRINGS: string[] = [];
const EMPTY_OPTIMISTIC: import("@/stores/chat").OptimisticMessage[] = [];

export function MessageList({
  conversationId,
  onReply,
  onEdit,
}: {
  conversationId: string;
  onReply: (m: Message) => void;
  onEdit: (id: string, text: string) => void;
}) {
  const scrollRef = React.useRef<HTMLDivElement>(null);
  const [atBottom, setAtBottom] = React.useState(true);
  const messagesQuery = useInfiniteMessages(conversationId);
  const typing = useChatStore((s) => s.typing[conversationId] ?? EMPTY_STRINGS);
  const optimistic = useChatStore((s) => s.optimistic[conversationId] ?? EMPTY_OPTIMISTIC);
  const failOptimistic = useChatStore((s) => s.failOptimistic);
  const resolveOptimistic = useChatStore((s) => s.resolveOptimistic);
  const removeOptimistic = useChatStore((s) => s.removeOptimistic);

  const messages = flattenMessages(messagesQuery.data?.pages);

  // Build display rows (day separators + grouping).
  const rows = React.useMemo(() => {
    const out: Row[] = [];
    let lastDay = "";
    let last: Message | null = null;
    for (const m of messages) {
      const day = new Date(m.created_at).toDateString();
      if (day !== lastDay) {
        out.push({ kind: "day", key: `day-${day}`, label: formatDayLabel(m.created_at) });
        lastDay = day;
        last = null;
      }
      const grouped =
        last !== null &&
        last.sender?.id === m.sender?.id &&
        m.type !== "system" &&
        last.type !== "system" &&
        Date.parse(m.created_at) - Date.parse(last.created_at) < GROUP_WINDOW_MS;
      out.push({ kind: "msg", key: m.id, message: m, grouped });
      last = m;
    }
    // Optimistic pending messages at the end.
    for (const om of optimistic) {
      const day = new Date(om.created_at).toDateString();
      if (day !== lastDay) {
        out.push({ kind: "day", key: `day-${day}`, label: formatDayLabel(om.created_at) });
        lastDay = day;
        last = null;
      }
      out.push({ kind: "msg", key: om.client_message_id ?? om.id, message: om, grouped: false });
    }
    if (typing.length > 0) out.push({ kind: "typing", key: "typing" });
    return out;
  }, [messages, optimistic, typing]);

  const virtualizer = useVirtualizer({
    count: rows.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => 68,
    overscan: 12,
    getItemKey: (i) => rows[i].key,
  });

  // Stick to bottom on first load.
  const initialScroll = React.useRef(false);
  React.useEffect(() => {
    if (!initialScroll.current && messages.length > 0) {
      initialScroll.current = true;
      requestAnimationFrame(() => {
        const el = scrollRef.current;
        if (el) el.scrollTop = el.scrollHeight;
      });
    }
  }, [messages.length]);

  // Follow new messages while at the bottom.
  const lastCount = React.useRef(0);
  React.useEffect(() => {
    const total = messages.length + optimistic.length;
    if (total > lastCount.current && atBottom) {
      requestAnimationFrame(() => {
        const el = scrollRef.current;
        if (el) el.scrollTop = el.scrollHeight;
      });
    }
    lastCount.current = total;
  }, [messages.length, optimistic.length, atBottom]);

  // Load older pages when nearing the top, preserving scroll position.
  const loadingOlder = React.useRef(false);
  React.useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    const onScroll = () => {
      const nearBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 120;
      setAtBottom(nearBottom);
      if (
        el.scrollTop < 600 &&
        !loadingOlder.current &&
        messagesQuery.hasPreviousPage &&
        !messagesQuery.isFetchingPreviousPage
      ) {
        loadingOlder.current = true;
        const prevHeight = el.scrollHeight;
        const prevTopMessage = messages[0]?.id;
        messagesQuery.fetchPreviousPage().then(() => {
          requestAnimationFrame(() => {
            const delta = el.scrollHeight - prevHeight;
            // Keep the previously-first message under the cursor.
            const prevEl = prevTopMessage ? document.querySelector(`[data-mid="${prevTopMessage}"]`) : null;
            if (prevEl) {
              const rect = prevEl.getBoundingClientRect();
              el.scrollTop = el.scrollTop + rect.top - 80;
            } else if (delta > 0) {
              el.scrollTop += delta;
            }
          });
          loadingOlder.current = false;
        }).catch(() => {
          loadingOlder.current = false;
        });
      }
    };
    el.addEventListener("scroll", onScroll, { passive: true });
    return () => el.removeEventListener("scroll", onScroll);
  }, [messagesQuery, messages]);

  return (
    <div className="relative min-h-0 flex-1">
      <div ref={scrollRef} className="absolute inset-0 overflow-y-auto overscroll-contain">
        <div className="mx-auto w-full max-w-[860px] px-3 sm:px-6">
          <div style={{ height: virtualizer.getTotalSize(), position: "relative" }}>
            {virtualizer.getVirtualItems().map((vItem) => {
              const row = rows[vItem.index];
              return (
                <div
                  key={vItem.key}
                  data-index={vItem.index}
                  ref={virtualizer.measureElement}
                  style={{
                    position: "absolute",
                    top: 0,
                    left: 0,
                    width: "100%",
                    transform: `translateY(${vItem.start}px)`,
                  }}
                >
                  {row.kind === "day" ? (
                    <div className="flex justify-center py-3">
                      <span className="rounded-full bg-surface-2 px-3 py-1 text-[11px] font-medium text-muted">
                        {row.label}
                      </span>
                    </div>
                  ) : row.kind === "typing" ? (
                    <div className="px-2 py-2">
                      <div className="inline-flex items-center gap-1.5 rounded-2xl bg-surface px-3.5 py-2.5 shadow-sm">
                        <span className="vy-typing-dot h-1.5 w-1.5 rounded-full bg-muted" />
                        <span className="vy-typing-dot h-1.5 w-1.5 rounded-full bg-muted" />
                        <span className="vy-typing-dot h-1.5 w-1.5 rounded-full bg-muted" />
                      </div>
                    </div>
                  ) : (
                    <div data-mid={row.message.id}>
                      <MessageBubble
                        message={row.message}
                        grouped={row.grouped}
                        onReply={onReply}
                        onEdit={onEdit}
                        onResolved={(clientId, serverId) => {
                          resolveOptimistic(conversationId, clientId, serverId);
                        }}
                        onFailed={(clientId) => failOptimistic(conversationId, clientId)}
                        onRetryRemoved={(clientId) => removeOptimistic(conversationId, clientId)}
                      />
                    </div>
                  )}
                </div>
              );
            })}
          </div>
          <div className="h-4" />
        </div>
      </div>

      {!atBottom && (
        <button
          onClick={() => {
            const el = scrollRef.current;
            if (el) el.scrollTo({ top: el.scrollHeight, behavior: "smooth" });
          }}
          className="absolute bottom-4 right-4 flex h-10 w-10 items-center justify-center rounded-full bg-surface text-foreground shadow-lg border border-border hover:bg-surface-2 transition-colors"
          aria-label="Jump to latest"
        >
          <ArrowDown size={18} />
        </button>
      )}
    </div>
  );
}

export function ReceiptTicks({ message, readSeq }: { message: Message; readSeq: number }) {
  if (!message.is_mine || message.type === "system" || message.pending) return null;
  const receipts = message.receipts;
  const allRead = receipts ? receipts.read_count >= receipts.total && receipts.total > 0 : false;
  const someDelivered = receipts ? receipts.delivered_count > 0 : false;

  if (allRead) {
    return (
      <span className="inline-flex items-center gap-0.5 text-[#9db4ff]" aria-label="Read">
        <CheckCheck size={14} />
      </span>
    );
  }
  if (someDelivered) {
    return (
      <span className="inline-flex items-center gap-0.5 opacity-80" aria-label="Delivered">
        <CheckCheck size={14} />
      </span>
    );
  }
  return (
    <span className="inline-flex items-center opacity-60" aria-label="Sent">
      <Check size={14} />
    </span>
  );
}
