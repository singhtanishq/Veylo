"use client";

// New conversation dialog: search people (username/email/fuzzy), start a
// direct conversation or create a group from a selection.

import * as React from "react";
import { useRouter } from "next/navigation";
import { Search, Users } from "lucide-react";
import { toast } from "sonner";
import { api, ApiError } from "@/lib/api";
import { Avatar } from "@/components/ui/avatar";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogTrigger } from "@/components/ui/overlays";
import { useMe } from "@/hooks/use-queries";

export function NewChatDialog({
  open,
  onOpenChange,
  onStarted,
  trigger,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  onStarted?: (conversationId: string) => void;
  trigger?: React.ReactNode;
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      {trigger ? <DialogTrigger asChild>{trigger}</DialogTrigger> : null}
      <DialogContent
        title="New conversation"
        description="Find people by @username, name, email — or start a group."
        className="max-w-md"
      >
        <NewChatBody onOpenChange={onOpenChange} onStarted={onStarted} />
      </DialogContent>
    </Dialog>
  );
}

function NewChatBody({
  onOpenChange,
  onStarted,
}: {
  onOpenChange: (v: boolean) => void;
  onStarted?: (conversationId: string) => void;
}) {
  const router = useRouter();
  const { data: me } = useMe();
  const [query, setQuery] = React.useState("");
  const [results, setResults] = React.useState<
    { id: string; display_name: string; username: string; avatar_url: string | null; is_contact: boolean }[]
  >([]);
  const [searching, setSearching] = React.useState(false);
  const [selected, setSelected] = React.useState<Record<string, typeof results[number]>>({});
  const [groupMode, setGroupMode] = React.useState(false);
  const [groupTitle, setGroupTitle] = React.useState("");
  const [busy, setBusy] = React.useState(false);

  React.useEffect(() => {
    if (query.trim().length < 1) {
      setResults([]);
      return;
    }
    setSearching(true);
    const t = setTimeout(async () => {
      try {
        const users = await api.searchUsers(query.trim());
        setResults(users.filter((u) => u.id !== me?.id));
      } catch {
        setResults([]);
      } finally {
        setSearching(false);
      }
    }, 300);
    return () => clearTimeout(t);
  }, [query, me?.id]);

  function toggle(u: (typeof results)[number]) {
    setSelected((s) => {
      const next = { ...s };
      if (next[u.id]) delete next[u.id];
      else next[u.id] = u;
      return next;
    });
  }

  async function startDirect(userId: string) {
    try {
      const conv = await api.directConversation(userId);
      onOpenChange(false);
      setQuery("");
      setSelected({});
      setGroupMode(false);
      onStarted?.(conv.id);
      router.push(`/?c=${conv.id}`);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "Could not start conversation");
    }
  }

  async function createGroup() {
    const ids = Object.keys(selected);
    if (!groupTitle.trim() || ids.length === 0) return;
    setBusy(true);
    try {
      const conv = await api.createGroup({ title: groupTitle.trim(), member_ids: ids });
      toast.success("Group created 🎉");
      onOpenChange(false);
      setGroupTitle("");
      setSelected({});
      setGroupMode(false);
      onStarted?.(conv.id);
      router.push(`/?c=${conv.id}`);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "Could not create group");
    } finally {
      setBusy(false);
    }
  }

  const selectedList = Object.values(selected);
  const selectionMode = selectedList.length > 0;

  return (
    <div>
      <div className="relative">
        <Search size={15} className="absolute left-3 top-1/2 -translate-y-1/2 text-muted" />
        <input
          autoFocus
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder={selectionMode && groupMode ? "Search more people…" : "Search @username, name or email"}
          className="h-10 w-full rounded-xl bg-surface-2 pl-9 pr-3 text-sm placeholder:text-muted/70 focus:outline-none focus:ring-2 focus:ring-ring/40"
        />
      </div>

      {/* Selection bar (group mode) */}
      {selectionMode && (
        <div className="mt-3 flex flex-wrap items-center gap-1.5 rounded-xl bg-primary-soft/60 p-2">
          {selectedList.map((u) => (
            <button
              key={u.id}
              onClick={() => toggle(u)}
              className="flex items-center gap-1.5 rounded-full bg-surface px-2 py-1 text-xs shadow-sm hover:bg-surface-2"
            >
              <Avatar src={u.avatar_url} name={u.display_name} userId={u.id} size={18} />
              {u.display_name.split(" ")[0]}
              <span className="text-muted">✕</span>
            </button>
          ))}
          {!groupMode ? (
            <>
              <input
                value={groupTitle}
                onChange={(e) => setGroupTitle(e.target.value)}
                placeholder="Group name…"
                className="h-7 min-w-32 flex-1 rounded-lg border border-border bg-surface px-2.5 text-xs focus:outline-none focus:ring-2 focus:ring-ring/40"
              />
              <Button size="sm" onClick={createGroup} loading={busy} disabled={!groupTitle.trim()}>
                <Users size={13} /> Create group
              </Button>
            </>
          ) : (
            <Button size="sm" onClick={createGroup} loading={busy} disabled={!groupTitle.trim()}>
              Create group
            </Button>
          )}
        </div>
      )}

      {/* Results */}
      <div className="mt-3 max-h-[280px] space-y-0.5 overflow-y-auto">
        {searching && <p className="py-6 text-center text-xs text-muted">Searching…</p>}
        {!searching && query.trim() && results.length === 0 && (
          <p className="py-8 text-center text-xs text-muted">
            No people found. Try @username or an email address.
          </p>
        )}
        {!searching && !query.trim() && (
          <p className="px-2 py-6 text-center text-xs text-muted leading-relaxed">
            Search for people to start chatting. Select several to create a group.
          </p>
        )}
        {results.map((u) => (
          <div key={u.id} className="flex items-center gap-2 rounded-xl px-1.5 py-1.5 hover:bg-surface-2">
            <button onClick={() => toggle(u)} className="flex min-w-0 flex-1 items-center gap-3 text-left">
              <Avatar src={u.avatar_url} name={u.display_name} userId={u.id} size={38} />
              <span className="min-w-0">
                <span className="block truncate text-[13.5px] font-medium">{u.display_name}</span>
                <span className="block truncate text-[11.5px] text-muted">
                  @{u.username}
                  {u.is_contact && <span className="ml-1.5 text-primary">· contact</span>}
                </span>
              </span>
            </button>
            {selectionMode && (
              <input
                type="checkbox"
                checked={!!selected[u.id]}
                onChange={() => toggle(u)}
                className="h-4 w-4 accent-[var(--primary)]"
                aria-label={`Select ${u.display_name}`}
              />
            )}
            <Button size="sm" variant="soft" onClick={() => startDirect(u.id)} disabled={selectionMode}>
              Message
            </Button>
          </div>
        ))}
      </div>
    </div>
  );
}

export function CreateGroupButton() {
  const [open, setOpen] = React.useState(false);
  const router = useRouter();
  return (
    <NewChatDialog
      open={open}
      onOpenChange={setOpen}
      onStarted={(id) => router.push(`/?c=${id}`)}
      trigger={
        <Button variant="soft">
          <Users size={16} /> New group
        </Button>
      }
    />
  );
}
