"use client";

// Message composer: autosizing textarea, emoji picker, attachments with
// progress, reply/edit banners, Enter-to-send, typing indicators, and
// optimistic sends with retry-safe client message IDs.

import * as React from "react";
import { Send, Smile, Paperclip, X, FileText } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Popover, PopoverTrigger, PopoverContent } from "@/components/ui/overlays";
import { EmojiPicker } from "@/components/chat/emoji-picker";
import { api } from "@/lib/api";
import { realtime } from "@/lib/ws";
import { useChatStore, type OptimisticMessage } from "@/stores/chat";
import { useConversation } from "@/hooks/use-queries";
import { cn, formatBytes, newClientMessageId } from "@/lib/utils";
import type { AttachmentMeta } from "@/lib/types";

export function Composer({
  conversationId,
  replyTo,
  onClearReply,
  editing,
  onClearEditing,
  onEdit,
}: {
  conversationId: string;
  replyTo: { id: string; preview: string; sender: string } | null;
  onClearReply: () => void;
  editing: { id: string; text: string } | null;
  onClearEditing: () => void;
  onEdit: (id: string, text: string) => Promise<void>;
}) {
  const [text, setText] = React.useState("");
  const [attachments, setAttachments] = React.useState<AttachmentMeta[]>([]);
  const [uploading, setUploading] = React.useState<{ name: string; pct: number } | null>(null);
  const textareaRef = React.useRef<HTMLTextAreaElement>(null);
  const fileRef = React.useRef<HTMLInputElement>(null);

  const addOptimistic = useChatStore((s) => s.addOptimistic);
  const resolveOptimistic = useChatStore((s) => s.resolveOptimistic);
  const failOptimistic = useChatStore((s) => s.failOptimistic);
  const notifyTypingSelf = useChatStore((s) => s.notifyTypingSelf);
  const { data: conversation } = useConversation(conversationId);

  // Focus when reply/edit banners change.
  React.useEffect(() => {
    if (editing) {
      setText(editing.text);
      textareaRef.current?.focus();
    } else if (replyTo) {
      textareaRef.current?.focus();
    }
  }, [editing, replyTo]);

  const [editId, setEditId] = React.useState<string | null>(null);

  // Autosize.
  React.useEffect(() => {
    const el = textareaRef.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = Math.min(el.scrollHeight, 160) + "px";
  }, [text]);

  function sendTyping(stop: boolean) {
    realtime.send(stop ? "typing.stop" : "typing.start", { conversation_id: conversationId });
  }

  function onChange(v: string) {
    setText(v);
    if (!editId) {
      notifyTypingSelf(conversationId, sendTyping);
    }
  }

  const canSend = (text.trim().length > 0 || attachments.length > 0) && !uploading;

  async function submit() {
    if (editId) {
      const t = text.trim();
      if (!t) return;
      try {
        await onEdit(editId, t);
        setEditId(null);
        setText("");
        onClearEditing();
        toast.success("Message updated");
      } catch (err) {
        toast.error(err instanceof Error ? err.message : "Edit failed");
      }
      return;
    }

    if (!canSend) return;
    const clientId = newClientMessageId();
    const now = new Date().toISOString();

    const optimistic: OptimisticMessage = {
      id: `pending-${clientId}`,
      conversation_id: conversationId,
      sender: conversation?.members?.find((m) => m.is_me)
        ? { id: conversation.members.find((m) => m.is_me)!.id, username: "", display_name: "You", avatar_url: null }
        : null,
      type: attachments.some((a) => a.kind === "image") ? "image" : attachments.length > 0 ? "document" : "text",
      text: text.trim() || null,
      seq: Number.MAX_SAFE_INTEGER,
      reply_to: replyTo
        ? { id: replyTo.id, sender_name: replyTo.sender, type: "text", preview: replyTo.preview }
        : null,
      attachments: attachments.length > 0 ? attachments : null,
      reactions: null,
      created_at: now,
      edited_at: null,
      deleted: false,
      is_mine: true,
      receipts: null,
      client_message_id: clientId,
      pending: "sending",
    };
    addOptimistic(conversationId, optimistic);

    const sendText = text.trim();
    const sendAttachments = attachments;
    setText("");
    setAttachments([]);
    onClearReply();
    realtime.send("typing.stop", { conversation_id: conversationId });

    try {
      const sent = await api.sendMessage(conversationId, {
        type: optimistic.type,
        text: sendText,
        reply_to_id: replyTo?.id,
        attachment_ids: sendAttachments.map((a) => a.id),
        client_message_id: clientId,
      });
      resolveOptimistic(conversationId, clientId, sent.id);
    } catch (err) {
      failOptimistic(conversationId, clientId);
      toast.error(err instanceof Error ? err.message : "Message failed to send");
    }
  }

  async function pickFiles(files: FileList | null) {
    if (!files || files.length === 0) return;
    for (const file of Array.from(files).slice(0, 5)) {
      setUploading({ name: file.name, pct: 0 });
      try {
        const att = await api.upload(file, (pct) => setUploading({ name: file.name, pct }));
        setAttachments((a) => [...a, att]);
      } catch (err) {
        toast.error(err instanceof Error ? err.message : "Upload failed");
      } finally {
        setUploading(null);
      }
    }
    if (fileRef.current) fileRef.current.value = "";
  }

  return (
    <div className="shrink-0 border-t border-border bg-surface px-3 pb-3 pt-2 sm:px-4">
      <div className="mx-auto w-full max-w-[860px]">
        {/* Reply banner */}
        {replyTo && !editId && (
          <div className="mb-2 flex items-center gap-2 rounded-xl bg-surface-2 px-3 py-2">
            <div className="min-w-0 flex-1 border-l-2 border-primary pl-2.5">
              <p className="text-[11px] font-semibold text-primary">Replying to {replyTo.sender}</p>
              <p className="truncate text-xs text-muted">{replyTo.preview}</p>
            </div>
            <button onClick={onClearReply} aria-label="Cancel reply" className="text-muted hover:text-foreground">
              <X size={16} />
            </button>
          </div>
        )}

        {/* Edit banner */}
        {editId && (
          <div className="mb-2 flex items-center gap-2 rounded-xl bg-primary-soft px-3 py-2">
            <div className="min-w-0 flex-1 border-l-2 border-primary pl-2.5">
              <p className="text-[11px] font-semibold text-primary">Editing message</p>
              <p className="truncate text-xs text-muted">Save with Enter, cancel with Esc</p>
            </div>
            <button
              onClick={() => {
                setEditId(null);
                setText("");
                onClearEditing();
              }}
              aria-label="Cancel edit"
              className="text-muted hover:text-foreground"
            >
              <X size={16} />
            </button>
          </div>
        )}

        {/* Attachment chips */}
        {attachments.length > 0 && (
          <div className="mb-2 flex flex-wrap gap-2">
            {attachments.map((a) => (
              <div key={a.id} className="group relative flex items-center gap-2 rounded-xl bg-surface-2 py-1.5 pl-2 pr-7">
                {a.kind === "image" && a.thumbnail_url ? (
                  // eslint-disable-next-line @next/next/no-img-element
                  <img src={`${process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080"}${a.thumbnail_url}`} alt={a.file_name} className="h-8 w-8 rounded-lg object-cover" />
                ) : (
                  <FileText size={16} className="text-primary" />
                )}
                <span className="max-w-36 truncate text-xs">{a.file_name}</span>
                <span className="text-[10px] text-muted">{formatBytes(a.size_bytes)}</span>
                <button
                  onClick={() => setAttachments((list) => list.filter((x) => x.id !== a.id))}
                  className="absolute right-1.5 text-muted hover:text-danger"
                  aria-label={`Remove ${a.file_name}`}
                >
                  <X size={13} />
                </button>
              </div>
            ))}
          </div>
        )}

        {/* Upload progress */}
        {uploading && (
          <div className="mb-2 rounded-xl bg-surface-2 px-3 py-2">
            <div className="flex items-center justify-between text-xs text-muted">
              <span className="truncate">Uploading {uploading.name}…</span>
              <span>{uploading.pct}%</span>
            </div>
            <div className="mt-1.5 h-1 overflow-hidden rounded-full bg-surface-3">
              <div className="h-full rounded-full bg-primary transition-all" style={{ width: `${uploading.pct}%` }} />
            </div>
          </div>
        )}

        {/* Input row */}
        <div className="flex items-end gap-1.5">
          <input
            ref={fileRef}
            type="file"
            multiple
            hidden
            onChange={(e) => pickFiles(e.target.files)}
            accept="image/*,video/*,audio/*,.pdf,.txt,.md,.csv,.json,.doc,.docx,.xls,.xlsx,.zip"
          />
          <Button
            size="icon"
            variant="ghost"
            onClick={() => fileRef.current?.click()}
            aria-label="Attach files"
            className="shrink-0"
            disabled={!!uploading}
          >
            <Paperclip size={18} />
          </Button>

          <Popover>
            <PopoverTrigger asChild>
              <Button size="icon" variant="ghost" aria-label="Emoji" className="shrink-0">
                <Smile size={18} />
              </Button>
            </PopoverTrigger>
            <PopoverContent align="start" className="w-auto p-2">
              <EmojiPicker onPick={(e) => setText((t) => t + e)} />
            </PopoverContent>
          </Popover>

          <textarea
            ref={textareaRef}
            value={text}
            onChange={(e) => onChange(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && !e.shiftKey) {
                e.preventDefault();
                submit();
              }
              if (e.key === "Escape" && editId) {
                setEditId(null);
                setText("");
                onClearEditing();
              }
            }}
            rows={1}
            placeholder="Write a message…"
            aria-label="Message input"
            className={cn(
              "max-h-40 min-h-[42px] flex-1 resize-none rounded-2xl border border-border bg-background px-4 py-2.5 text-[14.5px]",
              "placeholder:text-muted/70 focus:outline-none focus:ring-2 focus:ring-ring/40",
            )}
          />

          <Button
            size="icon"
            onClick={submit}
            disabled={!canSend && !editId}
            aria-label={editId ? "Save edit" : "Send message"}
            className="shrink-0"
          >
            <Send size={17} />
          </Button>
        </div>
      </div>
    </div>
  );
}
