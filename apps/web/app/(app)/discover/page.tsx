"use client";

// Discover: find people by @username, email or (verified, discoverable) phone
// and start conversations or send contact requests.

import * as React from "react";
import { useRouter } from "next/navigation";
import { Search, UserPlus, MessageSquare, SearchX } from "lucide-react";
import { toast } from "sonner";
import { api } from "@/lib/api";
import { Avatar } from "@/components/ui/avatar";
import { Button } from "@/components/ui/button";
import { useMe, useConversationMutation } from "@/hooks/use-queries";
import { lastSeenLabel } from "@/lib/utils";
import type { PublicUser } from "@/lib/types";

const SUGGESTIONS = ["Try @username", "name@example.test", "+919876543210"];

export default function DiscoverPage() {
  const [query, setQuery] = React.useState("");
  const [results, setResults] = React.useState<PublicUser[]>([]);
  const [searching, setSearching] = React.useState(false);
  const [searched, setSearched] = React.useState(false);
  const { data: me } = useMe();

  React.useEffect(() => {
    const q = query.trim();
    if (q.length < 1) {
      setResults([]);
      setSearched(false);
      return;
    }
    setSearching(true);
    const t = setTimeout(async () => {
      try {
        const users = await api.searchUsers(q);
        setResults(users.filter((u) => u.id !== me?.id));
        setSearched(true);
      } catch {
        setResults([]);
        setSearched(true);
      } finally {
        setSearching(false);
      }
    }, 300);
    return () => clearTimeout(t);
  }, [query, me?.id]);

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-y-auto">
      <div className="mx-auto w-full max-w-2xl px-6 py-8">
        <h1 className="text-xl font-semibold tracking-tight" style={{ fontFamily: "var(--font-sora)" }}>
          Discover people
        </h1>
        <p className="mt-1 text-sm text-muted">
          Find people on this Veylo instance. Search respects everyone&apos;s privacy settings.
        </p>

        <div className="relative mt-6">
          <Search size={17} className="absolute left-4 top-1/2 -translate-y-1/2 text-muted" />
          <input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search @username, name, email or phone"
            aria-label="Search people"
            className="h-12 w-full rounded-2xl border border-border bg-surface pl-11 pr-4 text-[15px] shadow-sm placeholder:text-muted/70 focus:outline-none focus:ring-2 focus:ring-ring/40"
          />
        </div>
        <div className="mt-2.5 flex flex-wrap gap-2">
          {SUGGESTIONS.map((s) => (
            <button
              key={s}
              onClick={() => setQuery(s)}
              className="rounded-full bg-surface-2 px-3 py-1 text-xs text-muted transition-colors hover:bg-surface-3 hover:text-foreground"
            >
              {s}
            </button>
          ))}
        </div>

        <div className="mt-8 space-y-2">
          {searching && <ResultSkeletons />}
          {!searching && searched && results.length === 0 && (
            <div className="flex flex-col items-center gap-3 py-16 text-center">
              <SearchX size={32} className="text-muted/50" />
              <p className="text-sm font-medium">No people found</p>
              <p className="max-w-xs text-xs text-muted leading-relaxed">
                The search may be private (email discoverability off, phone unverified) or the
                person may not exist on this instance.
              </p>
            </div>
          )}
          {!searching && !searched && (
            <div className="rounded-2xl bg-surface p-6 text-sm text-muted leading-relaxed shadow-sm">
              <p className="font-medium text-foreground">How discovery works</p>
              <ul className="mt-2 space-y-1.5">
                <li>• <span className="text-foreground">@username</span> — everyone is searchable by username</li>
                <li>• Email search only finds people with email discoverability enabled</li>
                <li>• Phone search only matches <span className="text-foreground">verified</span> numbers</li>
                <li>• People who blocked you never appear in results</li>
              </ul>
            </div>
          )}
          {results.map((u) => (
            <PersonCard key={u.id} user={u} />
          ))}
        </div>
      </div>
    </div>
  );
}

function ResultSkeletons() {
  return (
    <div className="space-y-2">
      {Array.from({ length: 3 }).map((_, i) => (
        <div key={i} className="flex items-center gap-3 rounded-2xl bg-surface p-4 shadow-sm">
          <div className="vy-skeleton h-12 w-12 rounded-full" />
          <div className="flex-1 space-y-2">
            <div className="vy-skeleton h-4 w-40" />
            <div className="vy-skeleton h-3 w-24" />
          </div>
        </div>
      ))}
    </div>
  );
}

function PersonCard({ user: u }: { user: PublicUser }) {
  const router = useRouter();
  const [requested, setRequested] = React.useState(false);
  const [isContact, setIsContact] = React.useState(u.is_contact);
  const sendRequest = useConversationMutation(async () => {
    await api.sendContactRequest(u.id);
  }, { invalidate: ["contactRequests", "notifications"] });
  const { data: me } = useMe();

  async function message() {
    try {
      const conv = await api.directConversation(u.id);
      router.push(`/?c=${conv.id}`);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Could not start conversation");
    }
  }

  return (
    <div className="flex items-center gap-4 rounded-2xl bg-surface p-4 shadow-sm transition-shadow hover:shadow">
      <Avatar src={u.avatar_url} name={u.display_name} userId={u.id} size={48} online={u.online} showPresence />
      <div className="min-w-0 flex-1">
        <p className="truncate text-[15px] font-semibold">
          {u.display_name}
          {isContact && <span className="ml-2 rounded-full bg-primary-soft px-2 py-0.5 text-[10px] font-medium text-primary">Contact</span>}
        </p>
        <p className="truncate text-xs text-muted">@{u.username}</p>
        {u.about && <p className="mt-0.5 truncate text-xs text-muted/90">{u.about}</p>}
        {!u.online && u.last_seen_at && (
          <p className="text-[11px] text-muted/70">{lastSeenLabel(u.last_seen_at)}</p>
        )}
      </div>
      <div className="flex shrink-0 items-center gap-2">
        {!isContact ? (
          <Button
            size="sm"
            variant={requested ? "ghost" : "outline"}
            disabled={requested || sendRequest.isPending}
            onClick={async () => {
              try {
                const res = await api.sendContactRequest(u.id);
                const status = (res as { status?: string }).status;
                if (status === "accepted") {
                  setIsContact(true);
                  toast.success("You are now contacts");
                } else {
                  setRequested(true);
                  toast.success("Contact request sent");
                }
              } catch (err) {
                toast.error(err instanceof Error ? err.message : "Could not send request");
              }
            }}
          >
            <UserPlus size={14} />
            {requested ? "Requested" : "Add"}
          </Button>
        ) : null}
        <Button size="sm" onClick={message}>
          <MessageSquare size={14} /> Message
        </Button>
      </div>
    </div>
  );
}
