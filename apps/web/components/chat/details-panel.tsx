"use client";

// Slide-over conversation details: profile header, group management (members,
// roles, add/remove), media gallery, mute/pin/archive quick toggles.

import * as React from "react";
import {
  X,
  Users,
  Image as ImageIcon,
  FileText,
  Phone,
  AtSign,
  Shield,
  UserPlus,
  MoreVertical,
  Crown,
  ShieldCheck,
} from "lucide-react";
import { toast } from "sonner";
import { Avatar } from "@/components/ui/avatar";
import { Button } from "@/components/ui/button";
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/overlays";
import {
  Dropdown,
  DropdownTrigger,
  DropdownContent,
  DropdownItem,
} from "@/components/ui/overlays";
import {
  useConversation,
  useGallery,
  useMe,
  useConversationMutation,
} from "@/hooks/use-queries";
import { api, attachmentUrl } from "@/lib/api";
import { cn, formatBytes, formatRelative, lastSeenLabel } from "@/lib/utils";
import type { Member, GalleryItem } from "@/lib/types";

export function DetailsPanel({
  conversationId,
  onClose,
}: {
  conversationId: string;
  onClose: () => void;
}) {
  const { data: c } = useConversation(conversationId);
  const { data: me } = useMe();
  const [addOpen, setAddOpen] = React.useState(false);

  if (!c) {
    return (
      <aside className="hidden md:flex w-[320px] shrink-0 flex-col border-l border-border bg-surface">
        <div className="vy-skeleton m-4 h-64 rounded-2xl" />
      </aside>
    );
  }

  const isGroup = c.type === "group";
  const myRole = c.my_role;
  const canManage = isGroup && (myRole === "owner" || myRole === "admin");

  return (
    <aside className="hidden md:flex w-[320px] shrink-0 flex-col border-l border-border bg-surface">
      <header className="flex items-center justify-between px-4 py-3.5 border-b border-border">
        <h2 className="text-[15px] font-semibold" style={{ fontFamily: "var(--font-sora)" }}>
          {isGroup ? "Group details" : "Profile"}
        </h2>
        <button
          onClick={onClose}
          className="flex h-8 w-8 items-center justify-center rounded-lg text-muted hover:bg-surface-2 hover:text-foreground"
          aria-label="Close details"
        >
          <X size={17} />
        </button>
      </header>

      <div className="min-h-0 flex-1 overflow-y-auto">
        {/* Identity block */}
        <div className="flex flex-col items-center px-6 py-6 text-center">
          {isGroup ? (
            <div className="flex h-20 w-20 items-center justify-center rounded-3xl bg-primary-soft text-primary">
              <Users size={34} />
            </div>
          ) : (
            <Avatar
              src={c.other_user?.avatar_url}
              name={c.other_user?.display_name ?? ""}
              userId={c.other_user?.id ?? c.id}
              size={80}
            />
          )}
          <h3 className="mt-3 text-[17px] font-semibold" style={{ fontFamily: "var(--font-sora)" }}>
            {isGroup ? c.title : c.other_user?.display_name}
          </h3>
          {!isGroup && (
            <p className="text-xs text-muted">@{c.other_user?.username}</p>
          )}
          {isGroup && c.description && (
            <p className="mt-2 text-sm text-muted">{c.description}</p>
          )}
          {!isGroup && c.other_user?.about && (
            <p className="mt-2 text-sm text-muted">{c.other_user.about}</p>
          )}
          {!isGroup && (
            <p className="mt-1 text-[11px] text-muted">
              {c.other_user?.online ? "Online now" : lastSeenLabel(c.other_user?.last_seen_at ?? null)}
            </p>
          )}
        </div>

        <Tabs defaultValue={isGroup ? "members" : "media"} className="px-3">
          <TabsList className="w-full">
            <TabsTrigger value={isGroup ? "members" : "media"} className="flex-1">
              {isGroup ? "Members" : "Media"}
            </TabsTrigger>
            <TabsTrigger value={isGroup ? "media" : "info"} className="flex-1">
              {isGroup ? "Media" : "Info"}
            </TabsTrigger>
          </TabsList>

          {isGroup && (
            <TabsContent value="members">
              <MembersTab
                conversationId={conversationId}
                members={c.members ?? []}
                canManage={canManage}
                myRole={myRole}
                meId={me?.id ?? ""}
                onAddClick={() => setAddOpen(true)}
              />
              {addOpen && (
                <AddMemberDialog conversationId={conversationId} existing={c.members ?? []} onClose={() => setAddOpen(false)} />
              )}
            </TabsContent>
          )}

          <TabsContent value="media">
            <GalleryTab conversationId={conversationId} />
          </TabsContent>

          {!isGroup && (
            <TabsContent value="info">
              <div className="space-y-1 py-1">
                <InfoRow icon={AtSign} label="Username" value={`@${c.other_user?.username}`} />
                <InfoRow icon={Shield} label="Role" value={roleLabel(myRole)} />
                <InfoRow icon={Users} label="Members" value={String(c.member_count)} />
              </div>
            </TabsContent>
          )}
        </Tabs>
      </div>
    </aside>
  );
}

function roleLabel(role: string) {
  return role === "owner" ? "Group owner" : role === "admin" ? "Group admin" : "Member";
}

function InfoRow({ icon: Icon, label, value }: { icon: React.ComponentType<{ size?: number; className?: string }>; label: string; value: string }) {
  return (
    <div className="flex items-center gap-3 rounded-xl px-3 py-2.5 hover:bg-surface-2">
      <Icon size={16} className="text-muted" />
      <div className="min-w-0">
        <p className="text-[11px] text-muted">{label}</p>
        <p className="truncate text-[13px] font-medium">{value}</p>
      </div>
    </div>
  );
}

function MembersTab({
  conversationId,
  members,
  canManage,
  myRole,
  meId,
  onAddClick,
}: {
  conversationId: string;
  members: Member[];
  canManage: boolean;
  myRole: string;
  meId: string;
  onAddClick: () => void;
}) {
  const removeMember = useConversationMutation(
    (vars: { userId: string }) => api.removeMember(conversationId, vars.userId),
    { invalidate: ["conversation"] },
  );
  const changeRole = useConversationMutation(
    (vars: { userId: string; role: string }) => api.changeRole(conversationId, vars.userId, vars.role),
    { invalidate: ["conversation"] },
  );

  return (
    <div className="py-2">
      {canManage && (
        <button
          onClick={onAddClick}
          className="mb-2 flex w-full items-center gap-3 rounded-xl px-3 py-2.5 text-primary hover:bg-primary-soft/60 transition-colors"
        >
          <span className="flex h-9 w-9 items-center justify-center rounded-full bg-primary-soft">
            <UserPlus size={17} />
          </span>
          <span className="text-[13px] font-medium">Add members</span>
        </button>
      )}
      <ul className="space-y-0.5">
        {members.map((m) => (
          <li key={m.id} className="flex items-center gap-3 rounded-xl px-3 py-2 hover:bg-surface-2 group">
            <Avatar src={m.avatar_url} name={m.display_name} userId={m.id} size={36} online={m.online} showPresence />
            <div className="min-w-0 flex-1">
              <p className="truncate text-[13px] font-medium">
                {m.display_name}
                {m.is_me && <span className="ml-1 text-[10px] text-muted">(you)</span>}
              </p>
              <p className="truncate text-[11px] text-muted">@{m.username}</p>
            </div>
            {m.role !== "member" && (
              <span className="flex items-center gap-1 rounded-full bg-surface-2 px-2 py-0.5 text-[10px] font-medium text-muted">
                {m.role === "owner" ? <Crown size={10} /> : <ShieldCheck size={10} />}
                {m.role}
              </span>
            )}
            {myRole === "owner" && !m.is_me && (
              <Dropdown>
                <DropdownTrigger asChild>
                  <button
                    className="flex h-7 w-7 items-center justify-center rounded-lg text-muted opacity-0 transition-opacity hover:bg-surface-3 group-hover:opacity-100"
                    aria-label={`Manage ${m.display_name}`}
                  >
                    <MoreVertical size={14} />
                  </button>
                </DropdownTrigger>
                <DropdownContent align="end">
                  {m.role === "member" && (
                    <DropdownItem onSelect={() => changeRole.mutate({ userId: m.id, role: "admin" })}>
                      <ShieldCheck size={14} /> Make admin
                    </DropdownItem>
                  )}
                  {m.role === "admin" && (
                    <DropdownItem onSelect={() => changeRole.mutate({ userId: m.id, role: "member" })}>
                      <Users size={14} /> Demote to member
                    </DropdownItem>
                  )}
                  <DropdownItem
                    danger
                    onSelect={() => removeMember.mutate({ userId: m.id })}
                  >
                    <X size={14} /> Remove from group
                  </DropdownItem>
                </DropdownContent>
              </Dropdown>
            )}
          </li>
        ))}
      </ul>
      <div className="mt-2 px-3">
        <Button
          variant="outline"
          className="w-full"
          onClick={async () => {
            try {
              await api.leaveConversation(conversationId);
              toast("You left the group");
            } catch (err) {
              toast.error(err instanceof Error ? err.message : "Could not leave group");
            }
          }}
        >
          Leave group
        </Button>
      </div>
    </div>
  );
}

function AddMemberDialog({
  conversationId,
  existing,
  onClose,
}: {
  conversationId: string;
  existing: Member[];
  onClose: () => void;
}) {
  const [query, setQuery] = React.useState("");
  const [results, setResults] = React.useState<{ id: string; display_name: string; username: string; avatar_url: string | null }[]>([]);
  const [selected, setSelected] = React.useState<Record<string, boolean>>({});
  const existingIds = new Set(existing.map((m) => m.id));
  const [busy, setBusy] = React.useState(false);

  React.useEffect(() => {
    if (query.trim().length < 1) {
      setResults([]);
      return;
    }
    const t = setTimeout(async () => {
      try {
        const users = await api.searchUsers(query.trim());
        setResults(users.filter((u) => !existingIds.has(u.id)));
      } catch {
        setResults([]);
      }
    }, 300);
    return () => clearTimeout(t);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [query]);

  async function add() {
    const ids = Object.keys(selected).filter((k) => selected[k]);
    if (ids.length === 0) return;
    setBusy(true);
    try {
      await api.addMembers(conversationId, ids);
      toast.success(`${ids.length} member${ids.length > 1 ? "s" : ""} added`);
      onClose();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Could not add members");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/45 p-4" onClick={onClose}>
      <div className="w-full max-w-sm rounded-2xl bg-surface p-5 shadow-2xl vy-animate-in" onClick={(e) => e.stopPropagation()}>
        <h3 className="text-base font-semibold" style={{ fontFamily: "var(--font-sora)" }}>Add members</h3>
        <input
          autoFocus
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="Search people by @username, name or email"
          className="mt-3 h-10 w-full rounded-xl bg-surface-2 px-3.5 text-sm focus:outline-none focus:ring-2 focus:ring-ring/40"
        />
        <div className="mt-3 max-h-64 overflow-y-auto space-y-0.5">
          {results.map((u) => (
            <label
              key={u.id}
              className="flex cursor-pointer items-center gap-3 rounded-xl px-2.5 py-2 hover:bg-surface-2"
            >
              <input
                type="checkbox"
                checked={!!selected[u.id]}
                onChange={(e) => setSelected((s) => ({ ...s, [u.id]: e.target.checked }))}
                className="h-4 w-4 accent-[var(--primary)]"
              />
              <Avatar src={u.avatar_url} name={u.display_name} userId={u.id} size={34} />
              <div className="min-w-0">
                <p className="truncate text-[13px] font-medium">{u.display_name}</p>
                <p className="truncate text-[11px] text-muted">@{u.username}</p>
              </div>
            </label>
          ))}
          {query.trim() && results.length === 0 && (
            <p className="px-2 py-6 text-center text-xs text-muted">No people found.</p>
          )}
        </div>
        <div className="mt-4 flex justify-end gap-2">
          <Button variant="ghost" onClick={onClose}>Cancel</Button>
          <Button onClick={add} loading={busy} disabled={Object.values(selected).every((v) => !v)}>
            Add
          </Button>
        </div>
      </div>
    </div>
  );
}

function GalleryTab({ conversationId }: { conversationId: string }) {
  const { data: items } = useGallery(conversationId);
  const [preview, setPreview] = React.useState<GalleryItem | null>(null);

  if (!items || items.length === 0) {
    return (
      <div className="flex flex-col items-center gap-2 py-10 text-center">
        <ImageIcon size={24} className="text-muted/50" />
        <p className="text-xs text-muted">No media shared yet.</p>
      </div>
    );
  }

  const images = items.filter((i) => i.attachment.kind === "image" || i.attachment.kind === "video");
  const documents = items.filter((i) => i.attachment.kind === "document" || i.attachment.kind === "audio" || i.attachment.kind === "voice");

  return (
    <div className="py-2">
      {images.length > 0 && (
        <>
          <p className="px-1 pb-1.5 text-[11px] font-semibold uppercase tracking-wide text-muted">Media</p>
          <div className="grid grid-cols-3 gap-1.5">
            {images.map((item) => (
              <button
                key={item.attachment.id}
                onClick={() => setPreview(item)}
                className="relative aspect-square overflow-hidden rounded-lg bg-surface-2"
              >
                {item.attachment.kind === "image" ? (
                  // eslint-disable-next-line @next/next/no-img-element
                  <img
                    src={attachmentUrl(item.attachment.thumbnail_url ?? item.attachment.download_url) ?? ""}
                    alt={item.attachment.name}
                    className="h-full w-full object-cover"
                  />
                ) : (
                  <video
                    src={attachmentUrl(item.attachment.download_url) ?? undefined}
                    className="h-full w-full object-cover"
                    preload="metadata"
                  />
                )}
              </button>
            ))}
          </div>
        </>
      )}
      {documents.length > 0 && (
        <>
          <p className="px-1 pb-1.5 pt-3 text-[11px] font-semibold uppercase tracking-wide text-muted">Files</p>
          <ul className="space-y-1">
            {documents.map((item) => (
              <li key={item.attachment.id}>
                <a
                  href={attachmentUrl(item.attachment.download_url) ?? "#"}
                  target="_blank"
                  rel="noreferrer"
                  className="flex items-center gap-2.5 rounded-xl px-2 py-2 hover:bg-surface-2"
                >
                  <FileText size={15} className="text-primary" />
                  <span className="min-w-0 flex-1 truncate text-[12.5px]">{item.attachment.name}</span>
                  <span className="text-[10px] text-muted">{formatBytes(item.attachment.size_bytes)}</span>
                </a>
              </li>
            ))}
          </ul>
        </>
      )}

      {/* Lightbox preview */}
      {preview && (
        <div
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/80 p-8"
          onClick={() => setPreview(null)}
        >
          <figure className="max-h-full max-w-3xl" onClick={(e) => e.stopPropagation()}>
            {preview.attachment.kind === "image" ? (
              // eslint-disable-next-line @next/next/no-img-element
              <img
                src={attachmentUrl(preview.attachment.download_url) ?? ""}
                alt={preview.attachment.name}
                className="max-h-[75vh] max-w-full rounded-xl object-contain"
              />
            ) : (
              <video
                src={attachmentUrl(preview.attachment.download_url) ?? undefined}
                controls
                autoPlay
                className="max-h-[75vh] rounded-xl"
              />
            )}
            <figcaption className="mt-3 flex items-center justify-between text-xs text-white/70">
              <span>
                {preview.attachment.name} · {formatBytes(preview.attachment.size_bytes)} · {formatRelative(preview.created_at)}
              </span>
              <a
                href={attachmentUrl(preview.attachment.download_url) ?? "#"}
                download
                className="rounded-lg bg-white/15 px-3 py-1.5 font-medium text-white hover:bg-white/25"
              >
                Download
              </a>
            </figcaption>
          </figure>
        </div>
      )}
    </div>
  );
}
