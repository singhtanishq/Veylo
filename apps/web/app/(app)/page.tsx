"use client";

// The Veylo workspace: conversation list + active chat. The selected
// conversation rides in the ?c= query param so any conversation is
// deep-linkable and screenshot-friendly.

import * as React from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { MessageSquarePlus, Sparkles } from "lucide-react";
import { ConversationList } from "@/components/conversations/conversation-list";
import { ChatWindow } from "@/components/chat/chat-window";
import { NewChatDialog } from "@/components/conversations/new-chat-dialog";
import { Button } from "@/components/ui/button";

export default function WorkspacePage() {
  const router = useRouter();
  const params = useSearchParams();
  const activeId = params.get("c");
  const [newChatOpen, setNewChatOpen] = React.useState(false);

  const select = React.useCallback(
    (id: string | null) => {
      if (id) router.push(`/?c=${id}`);
      else router.push("/");
    },
    [router],
  );

  return (
    <div className="flex h-full min-w-0">
      {/* Conversation list pane */}
      <section
        className={`flex w-full sm:w-[340px] md:w-[360px] shrink-0 flex-col border-r border-border bg-surface ${
          activeId ? "hidden sm:flex" : "flex"
        }`}
      >
        <header className="flex items-center justify-between px-4 pt-4 pb-2">
          <div>
            <h1 className="text-lg font-semibold tracking-tight" style={{ fontFamily: "var(--font-sora)" }}>
              Messages
            </h1>
            <p className="text-xs text-muted">Everything syncs in real time</p>
          </div>
          <Button size="icon" variant="soft" onClick={() => setNewChatOpen(true)} aria-label="New conversation">
            <MessageSquarePlus size={18} />
          </Button>
        </header>
        <ConversationList activeId={activeId} onSelect={select} />
      </section>

      {/* Chat pane */}
      <section className={`flex-1 min-w-0 ${activeId ? "flex" : "hidden sm:flex"}`}>
        {activeId ? (
          <ChatWindow key={activeId} conversationId={activeId} onClose={() => select(null)} />
        ) : (
          <EmptyWorkspace onNew={() => setNewChatOpen(true)} />
        )}
      </section>

      <NewChatDialog open={newChatOpen} onOpenChange={setNewChatOpen} onStarted={(id) => select(id)} />
    </div>
  );
}

function EmptyWorkspace({ onNew }: { onNew: () => void }) {
  return (
    <div className="flex flex-1 flex-col items-center justify-center gap-4 p-8 text-center">
      <div className="flex h-16 w-16 items-center justify-center rounded-3xl bg-primary-soft text-primary">
        <Sparkles size={28} />
      </div>
      <div>
        <h2 className="text-lg font-semibold" style={{ fontFamily: "var(--font-sora)" }}>
          Your conversations live here
        </h2>
        <p className="mt-1 max-w-sm text-sm text-muted">
          Pick a conversation on the left, or discover someone new and say hello. Messages deliver
          instantly and stay in sync across your sessions.
        </p>
      </div>
      <Button variant="soft" onClick={onNew}>
        Start a conversation
      </Button>
    </div>
  );
}
