"use client";

// Message bubble: system messages, text, media attachments, document cards,
// voice notes, replies, reactions, edit/delete/copy context menu, receipts.

import * as React from "react";
import Image from "next/image";
import {
  FileText,
  Copy,
  Reply,
  Pencil,
  Trash2,
  SmilePlus,
  Download,
  Play,
  AlertCircle,
  RotateCcw,
} from "lucide-react";
import { toast } from "sonner";
import { Avatar } from "@/components/ui/avatar";
import {
  Dropdown,
  DropdownTrigger,
  DropdownContent,
  DropdownItem,
} from "@/components/ui/overlays";
import { Popover, PopoverTrigger, PopoverContent } from "@/components/ui/overlays";
import { EmojiPicker } from "@/components/chat/emoji-picker";
import { api, attachmentUrl } from "@/lib/api";
import { useChatStore, type OptimisticMessage } from "@/stores/chat";
import { cn, formatTime, formatBytes } from "@/lib/utils";
import type { Message, AttachmentMeta } from "@/lib/types";

const QUICK_REACTIONS = ["👍", "❤️", "😂", "🎉", "😮", "😢"];

export function MessageBubble({
  message: m,
  grouped,
  onReply,
  onEdit,
  onResolved,
  onFailed,
  onRetryRemoved,
}: {
  message: Message | OptimisticMessage;
  grouped: boolean;
  onReply: (m: Message) => void;
  onEdit?: (id: string, text: string) => void;
  onResolved?: (clientId: string, serverId: string) => void;
  onFailed?: (clientId: string) => void;
  onRetryRemoved?: (clientId: string) => void;
}) {
  const addOptimistic = useChatStore((s) => s.addOptimistic);

  // System message style.
  if (m.type === "system") {
    return (
      <div className="flex justify-center py-1.5">
        <span className="rounded-full bg-surface-2/80 px-3 py-1 text-[11px] text-muted">
          {m.text}
        </span>
      </div>
    );
  }

  const mine = m.is_mine;
  const isPending = "pending" in m ? m.pending === "sending" : false;
  const isFailed = "pending" in m ? m.pending === "failed" : false;
  const senderName = m.sender?.display_name ?? "?";

  async function retry() {
    const clientId = m.client_message_id;
    if (!clientId || !m.conversation_id) return;
    onRetryRemoved?.(clientId);
    addOptimistic(m.conversation_id, { ...(m as OptimisticMessage), pending: "sending" });
    try {
      const sent = await api.sendMessage(m.conversation_id, {
        type: m.type,
        text: m.text ?? "",
        client_message_id: clientId,
        attachment_ids: (m.attachments ?? []).map((a) => a.id),
        reply_to_id: m.reply_to?.id,
      });
      onResolved?.(clientId, sent.id);
    } catch {
      onFailed?.(clientId);
      toast.error("Still failing — check your connection.");
    }
  }

  function menu(onEdit?: () => void, onDelete?: () => void) {
    return (
      <DropdownContent align={mine ? "end" : "start"} className="min-w-40">
        <DropdownItem onSelect={() => onReply(m)}>
          <Reply size={14} /> Reply
        </DropdownItem>
        {m.type === "text" && !m.deleted && (
          <DropdownItem
            onSelect={() => {
              navigator.clipboard.writeText(m.text ?? "").then(
                () => toast("Copied to clipboard"),
                () => toast.error("Could not copy"),
              );
            }}
          >
            <Copy size={14} /> Copy text
          </DropdownItem>
        )}
        {mine && m.type === "text" && onEdit && (
          <DropdownItem onSelect={onEdit}>
            <Pencil size={14} /> Edit
          </DropdownItem>
        )}
        {mine && !m.deleted && onDelete && (
          <DropdownItem danger onSelect={onDelete}>
            <Trash2 size={14} /> Delete
          </DropdownItem>
        )}
      </DropdownContent>
    );
  }

  const bubbleBody = (
    <BubbleContent message={m} mine={mine} />
  );

  return (
    <div
      className={cn(
        "group flex w-full gap-2.5 px-2 py-0.5",
        mine ? "flex-row-reverse" : "flex-row",
        grouped ? "pt-0.5" : "pt-2.5",
      )}
    >
      {!mine ? (
        <div className="w-8 shrink-0">
          {!grouped && (
            <Avatar src={m.sender?.avatar_url} name={senderName} userId={m.sender?.id ?? m.id} size={32} />
          )}
        </div>
      ) : null}

      <div className={cn("flex min-w-0 max-w-[76%] flex-col", mine ? "items-end" : "items-start")}>
        {/* Sender name for groups */}
        {!mine && !grouped && (
          <span className="mb-0.5 px-1 text-[11px] font-medium text-muted">{senderName}</span>
        )}

        <div className={cn("flex items-end gap-1.5", mine ? "flex-row" : "flex-row-reverse")}>
          {/* Reaction / hover actions (outside bubble) */}
          <div className="flex items-center gap-0.5 opacity-0 transition-opacity group-hover:opacity-100 focus-within:opacity-100">
            <Popover>
              <PopoverTrigger asChild>
                <button
                  className="flex h-7 w-7 items-center justify-center rounded-full text-muted hover:bg-surface-2 hover:text-foreground"
                  aria-label="Add reaction"
                >
                  <SmilePlus size={15} />
                </button>
              </PopoverTrigger>
              <PopoverContent className="w-auto p-2">
                <div className="flex gap-1">
                  {QUICK_REACTIONS.map((e) => (
                    <button
                      key={e}
                      className="flex h-8 w-8 items-center justify-center rounded-lg text-lg hover:bg-surface-2"
                      onClick={async () => {
                        try {
                          await api.react(m.id, e);
                        } catch {
                          toast.error("Could not react");
                        }
                      }}
                    >
                      {e}
                    </button>
                  ))}
                </div>
                <div className="mt-1.5 border-t border-border pt-1.5">
                  <EmojiPicker
                    onPick={async (emoji) => {
                      try {
                        await api.react(m.id, emoji);
                      } catch {
                        toast.error("Could not react");
                      }
                    }}
                  />
                </div>
              </PopoverContent>
            </Popover>
            <Dropdown>
              <DropdownTrigger asChild>
                <button
                  className="flex h-7 w-7 items-center justify-center rounded-full text-muted hover:bg-surface-2 hover:text-foreground"
                  aria-label="Message options"
                >
                  ⋯
                </button>
              </DropdownTrigger>
              {menu(
                mine && m.type === "text" && onEdit
                  ? () => onEdit(m.id, m.text ?? "")
                  : undefined,
                mine && !m.deleted
                  ? async () => {
                      try {
                        await api.deleteMessage(m.id);
                      } catch {
                        toast.error("Could not delete message");
                      }
                    }
                  : undefined,
              )}
            </Dropdown>
          </div>

          {/* The bubble */}
          <div
            className={cn(
              "relative min-w-0 rounded-2xl px-3.5 py-2.5 text-[14.5px] leading-relaxed shadow-sm",
              mine
                ? "bg-bubble-own text-bubble-own-foreground"
                : "bg-bubble-other text-bubble-other-foreground border border-border/60",
              isPending && "opacity-70",
              isFailed && "border border-danger/40",
              m.type === "image" && "p-1.5",
            )}
          >
            {m.reply_to && (
              <div
                className={cn(
                  "mb-2 rounded-lg border-l-2 px-2.5 py-1.5 text-xs",
                  mine ? "border-white/50 bg-white/10" : "border-primary/50 bg-surface-2",
                )}
              >
                <p className="font-semibold opacity-90">{m.reply_to.sender_name}</p>
                <p className="truncate opacity-75">{m.reply_to.preview || "…"}</p>
              </div>
            )}

            {bubbleBody}

            <div
              className={cn(
                "mt-0.5 flex items-center justify-end gap-1.5 text-[10.5px]",
                mine ? "text-white/70" : "text-muted",
              )}
            >
              {m.edited_at && <span className="italic">edited</span>}
              <span>{formatTime(m.created_at)}</span>
              {mine && !("pending" in m) && (
                <ReceiptTicksInline message={m} />
              )}
              {"pending" in m && (
                <span className={isFailed ? "text-[#ffb1b1]" : ""}>
                  {isFailed ? (
                    <button onClick={retry} className="inline-flex items-center gap-1 underline">
                      <AlertCircle size={11} /> Failed — retry
                    </button>
                  ) : (
                    <span className="inline-flex items-center gap-1">
                      <span className="h-2.5 w-2.5 animate-spin rounded-full border border-white/50 border-t-transparent" />
                      Sending
                    </span>
                  )}
                </span>
              )}
            </div>

            {/* Reactions row */}
            {m.reactions && m.reactions.length > 0 && (
              <div className={cn("mt-1.5 flex flex-wrap gap-1", mine && "justify-start")}>
                {m.reactions.map((r) => (
                  <button
                    key={r.emoji}
                    onClick={async () => {
                      try {
                        if (r.mine) await api.unreact(m.id, r.emoji);
                        else await api.react(m.id, r.emoji);
                      } catch {
                        toast.error("Could not update reaction");
                      }
                    }}
                    className={cn(
                      "flex items-center gap-1 rounded-full px-2 py-0.5 text-xs transition-colors",
                      r.mine
                        ? "bg-primary-soft text-primary border border-primary/30"
                        : "bg-surface-2 text-foreground/80",
                    )}
                    title={`${r.count} reaction${r.count > 1 ? "s" : ""}`}
                  >
                    <span>{r.emoji}</span>
                    <span className="font-semibold">{r.count}</span>
                  </button>
                ))}
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}

function ReceiptTicksInline({ message }: { message: Message }) {
  const receipts = message.receipts;
  const allRead = receipts ? receipts.read_count >= receipts.total && receipts.total > 0 : false;
  const someDelivered = receipts ? receipts.delivered_count > 0 : false;
  if (allRead) {
    return (
      <span className="text-[#9db4ff]" aria-label="Read by everyone">
        ✓✓
      </span>
    );
  }
  if (someDelivered) {
    return <span aria-label="Delivered">✓✓</span>;
  }
  return <span aria-label="Sent">✓</span>;
}

function BubbleContent({ message: m, mine }: { message: Message; mine: boolean }) {
  if (m.deleted) {
    return (
      <p className={cn("italic text-sm", mine ? "text-white/60" : "text-muted")}>
        This message was deleted
      </p>
    );
  }

  switch (m.type) {
    case "image":
    case "video":
      return (
        <MediaContent attachments={m.attachments ?? []} mine={mine} type={m.type}>
          {m.text && (
            <p className="whitespace-pre-wrap break-words px-1.5 pb-0.5 pt-1.5 text-[14.5px]">
              {m.text}
            </p>
          )}
        </MediaContent>
      );
    case "voice":
    case "audio":
      return <AudioContent attachments={m.attachments ?? []} mine={mine} />;
    case "document":
      return <DocumentContent attachments={m.attachments ?? []} mine={mine} />;
    default:
      return <p className="whitespace-pre-wrap break-words">{m.text}</p>;
  }
}

function MediaContent({
  attachments,
  mine,
  type,
  children,
}: {
  attachments: AttachmentMeta[];
  mine: boolean;
  type: string;
  children?: React.ReactNode;
}) {
  return (
    <div className="flex flex-col gap-1.5">
      {attachments.map((a) => {
        const url = attachmentUrl(a.download_url);
        if (type === "image" && url) {
          return (
            <a key={a.id} href={url} target="_blank" rel="noreferrer" className="block overflow-hidden rounded-xl">
              <Image
                src={url}
                alt={a.file_name}
                width={a.width ?? 480}
                height={a.height ?? 320}
                className="max-h-[320px] w-auto max-w-full rounded-xl object-cover transition-transform hover:scale-[1.01]"
                style={{ maxHeight: 320 }}
                unoptimized
              />
            </a>
          );
        }
        // Video
        return (
          <video
            key={a.id}
            src={url ?? undefined}
            controls
            className="max-h-[320px] w-auto max-w-full rounded-xl"
            preload="metadata"
          />
        );
      })}
      {attachments.length === 0 && (
        <p className={cn("text-sm", mine ? "text-white/70" : "text-muted")}>Attachment unavailable</p>
      )}
      {children}
    </div>
  );
}

function DocumentContent({ attachments, mine }: { attachments: AttachmentMeta[]; mine: boolean }) {
  return (
    <div className="flex flex-col gap-1.5">
      {attachments.map((a) => (
        <a
          key={a.id}
          href={attachmentUrl(a.download_url) ?? "#"}
          target="_blank"
          rel="noreferrer"
          className={cn(
            "flex items-center gap-3 rounded-xl px-3 py-2.5 transition-colors",
            mine ? "bg-white/10 hover:bg-white/20" : "bg-surface-2 hover:bg-surface-3",
          )}
        >
          <span className={cn("flex h-9 w-9 items-center justify-center rounded-lg", mine ? "bg-white/15" : "bg-surface")}>
            <FileText size={17} className={mine ? "text-white" : "text-primary"} />
          </span>
          <span className="min-w-0 flex-1">
            <span className="block truncate text-[13px] font-medium">{a.file_name}</span>
            <span className={cn("block text-[11px]", mine ? "text-white/60" : "text-muted")}>
              {formatBytes(a.size_bytes)}
            </span>
          </span>
          <Download size={16} className={mine ? "text-white/70" : "text-muted"} />
        </a>
      ))}
    </div>
  );
}

function AudioContent({ attachments, mine }: { attachments: AttachmentMeta[]; mine: boolean }) {
  return (
    <div className="flex flex-col gap-1.5">
      {attachments.map((a) => (
        <div key={a.id} className={cn("flex items-center gap-2 rounded-xl px-2 py-1.5", mine ? "bg-white/10" : "bg-surface-2")}>
          <span className={cn("flex h-8 w-8 items-center justify-center rounded-full", mine ? "bg-white/15" : "bg-surface")}>
            <Play size={14} className={mine ? "text-white" : "text-primary"} />
          </span>
          <audio
            src={attachmentUrl(a.download_url) ?? undefined}
            controls
            className="h-8 max-w-[220px]"
            preload="metadata"
          />
        </div>
      ))}
    </div>
  );
}
