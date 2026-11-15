"use client";

// The active conversation pane: header (presence, actions), virtualized
// message history, composer, and a slide-over details drawer.

import * as React from "react";
import {
  ArrowLeft,
  Info,
  MoreVertical,
  Pin,
  PinOff,
  VolumeX,
  Volume2,
  Archive,
  ArchiveRestore,
  Trash2,
  Flag,
} from "lucide-react";
import { toast } from "sonner";
import { Avatar } from "@/components/ui/avatar";
import { Button } from "@/components/ui/button";
import {
  Dropdown,
  DropdownTrigger,
  DropdownContent,
  DropdownItem,
  DropdownSeparator,
} from "@/components/ui/overlays";
import { DetailsPanel } from "@/components/chat/details-panel";
import { MessageList } from "@/components/chat/message-list";
import { Composer } from "@/components/chat/composer";
import {
  useConversation,
  useConversationMutation,
} from "@/hooks/use-queries";
import { useChatStore } from "@/stores/chat";
import { api } from "@/lib/api";
import { lastSeenLabel, cn } from "@/lib/utils";

export function ChatWindow({
  conversationId,
  onClose,
}: {
  conversationId: string;
  onClose: () => void;
}) {
  const { data: conversation, isLoading } = useConversation(conversationId);
  const otherPresence = useOtherPresence(conversation?.other_user?.id, conversation?.other_user?.online);
  const [detailsOpen, setDetailsOpen] = React.useState(false);
  const [replyTo, setReplyTo] = React.useState<{ id: string; preview: string; sender: string } | null>(null);
  const [editing, setEditing] = React.useState<{ id: string; text: string } | null>(null);

  const updateSettings = useConversationMutation(
    (vars: { is_muted?: boolean; is_pinned?: boolean; is_archived?: boolean }) =>
      api.updateMemberSettings(conversationId, vars),
  );
  async function deleteConversation() {
    try {
      if (conversation?.type === "group") {
        await api.deleteConversation(conversationId);
      } else {
        await api.leaveConversation(conversationId);
      }
      toast("Conversation removed");
      onClose();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Could not remove conversation");
    }
  }


  if (isLoading || !conversation) {
    return (
      <div className="flex flex-1 items-center justify-center">
        <div className="vy-skeleton h-24 w-64 rounded-2xl" />
      </div>
    );
  }

  const title = conversation.type === "group" ? conversation.title : conversation.other_user?.display_name;

  return (
    <div className="flex min-w-0 flex-1">
      <div className="flex min-w-0 flex-1 flex-col bg-background">
        {/* Header */}
        <header className="flex h-[64px] shrink-0 items-center gap-3 border-b border-border bg-surface px-3 sm:px-4">
          <button
            onClick={onClose}
            className="flex h-9 w-9 items-center justify-center rounded-xl text-muted hover:bg-surface-2 hover:text-foreground sm:hidden"
            aria-label="Back to conversations"
          >
            <ArrowLeft size={18} />
          </button>

          <button
            className="flex min-w-0 flex-1 items-center gap-3 text-left"
            onClick={() => setDetailsOpen(true)}
            aria-label="Open conversation details"
          >
            {conversation.type === "group" ? (
              <Avatar
                src={conversation.avatar_url}
                name={conversation.title ?? "Group"}
                userId={conversation.id}
                size={40}
              />
            ) : (
              <Avatar
                src={conversation.other_user?.avatar_url}
                name={conversation.other_user?.display_name ?? title ?? "?"}
                userId={conversation.other_user?.id ?? conversation.id}
                size={40}
                online={otherPresence}
                showPresence
              />
            )}
            <div className="min-w-0">
              <p className="truncate text-[15px] font-semibold">{title}</p>
              <p className="truncate text-xs text-muted">
                {conversation.type === "group" ? (
                  `${conversation.member_count} members`
                ) : otherPresence ? (
                  <span className="text-success">Online</span>
                ) : (
                  lastSeenLabel(conversation.other_user?.last_seen_at ?? null) || "Offline"
                )}
              </p>
            </div>
          </button>

          <div className="flex items-center gap-0.5">
            <HeaderActions
              conversationId={conversationId}
              isMuted={conversation.is_muted}
              isPinned={conversation.is_pinned}
              isArchived={conversation.is_archived}
              onToggleMute={() => updateSettings.mutate({ is_muted: !conversation.is_muted })}
              onTogglePin={() => updateSettings.mutate({ is_pinned: !conversation.is_pinned })}
              onToggleArchive={() => updateSettings.mutate({ is_archived: !conversation.is_archived })}
              onOpenDetails={() => setDetailsOpen(true)}
              onDelete={deleteConversation}
              canDelete={conversation.type === "group" && conversation.my_role === "owner"}
            />
          </div>
        </header>

        {/* Messages */}
        <MessageList
          conversationId={conversationId}
          onReply={(m) => setReplyTo({ id: m.id, preview: m.text ?? "[attachment]", sender: m.sender?.display_name ?? "" })}
          onEdit={(id, text) => setEditing({ id, text })}
        />

        {/* Composer */}
        <Composer
          conversationId={conversationId}
          replyTo={replyTo}
          onClearReply={() => setReplyTo(null)}
          editing={editing}
          onClearEditing={() => setEditing(null)}
          onEdit={async (id, text) => {
            await api.editMessage(id, text);
          }}
        />
      </div>

      {/* Details drawer */}
      {detailsOpen && (
        <DetailsPanel conversationId={conversationId} onClose={() => setDetailsOpen(false)} />
      )}
    </div>
  );
}

// Presence from the live store, falling back to server data.
function useOtherPresence(userId: string | undefined, serverOnline: boolean | undefined) {
  const presence = useChatStore((s) => (userId ? s.presence[userId] : undefined));
  return presence ?? serverOnline ?? false;
}

function HeaderActions(props: {
  conversationId: string;
  isMuted: boolean;
  isPinned: boolean;
  isArchived: boolean;
  onToggleMute: () => void;
  onTogglePin: () => void;
  onToggleArchive: () => void;
  onOpenDetails: () => void;
  onDelete: () => void;
  canDelete: boolean;
}) {
  return (
    <>
      <Button
        size="icon"
        variant="ghost"
        onClick={props.onOpenDetails}
        aria-label="Conversation info"
        className="hidden sm:flex"
      >
        <Info size={18} />
      </Button>
      <Dropdown>
        <DropdownTrigger asChild>
          <Button size="icon" variant="ghost" aria-label="More options">
            <MoreVertical size={18} />
          </Button>
        </DropdownTrigger>
        <DropdownContent align="end">
          <DropdownItem onSelect={props.onTogglePin}>
            {props.isPinned ? <PinOff size={15} /> : <Pin size={15} />}
            {props.isPinned ? "Unpin conversation" : "Pin conversation"}
          </DropdownItem>
          <DropdownItem onSelect={props.onToggleMute}>
            {props.isMuted ? <Volume2 size={15} /> : <VolumeX size={15} />}
            {props.isMuted ? "Unmute notifications" : "Mute notifications"}
          </DropdownItem>
          <DropdownItem onSelect={props.onToggleArchive}>
            {props.isArchived ? <ArchiveRestore size={15} /> : <Archive size={15} />}
            {props.isArchived ? "Unarchive" : "Archive"}
          </DropdownItem>
          <DropdownSeparator />
          {props.canDelete ? (
            <DropdownItem danger onSelect={props.onDelete}>
              <Trash2 size={15} /> Delete group
            </DropdownItem>
          ) : (
            <DropdownItem danger onSelect={props.onDelete}>
              <Flag size={15} /> Remove conversation
            </DropdownItem>
          )}
        </DropdownContent>
      </Dropdown>
    </>
  );
}
