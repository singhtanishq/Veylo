"use client";

// Settings: profile, appearance, notifications, privacy, security (sessions +
// password), account. Sections separated with tabs; everything is live.

import * as React from "react";
import { useRouter } from "next/navigation";
import {
  User,
  Palette,
  Bell,
  ShieldCheck,
  Lock,
  MonitorSmartphone,
  LogOut,
  Camera,
  BadgeCheck,
} from "lucide-react";
import { toast } from "sonner";
import { api, ApiError, attachmentUrl } from "@/lib/api";
import { realtime } from "@/lib/ws";
import { Avatar } from "@/components/ui/avatar";
import { Button } from "@/components/ui/button";
import { Input, Label, Textarea } from "@/components/ui/input";
import { Tabs, TabsList, TabsTrigger, TabsContent, Switch } from "@/components/ui/overlays";
import {
  useMe,
  useSessions,
  usePrivacySettings,
  useNotificationSettings,
} from "@/hooks/use-queries";
import { useQueryClient } from "@tanstack/react-query";
import { cn, formatRelative } from "@/lib/utils";
import type { SessionInfo, PrivacySettings, NotificationSettings } from "@/lib/types";

export default function SettingsPage() {
  return (
    <div className="min-h-0 flex-1 overflow-y-auto">
      <div className="mx-auto w-full max-w-2xl px-6 py-8">
        <h1 className="text-xl font-semibold tracking-tight" style={{ fontFamily: "var(--font-sora)" }}>
          Settings
        </h1>

        <Tabs defaultValue="profile" className="mt-6">
          <TabsList className="flex-wrap">
            <TabsTrigger value="profile">Profile</TabsTrigger>
            <TabsTrigger value="appearance">Appearance</TabsTrigger>
            <TabsTrigger value="notifications">Notifications</TabsTrigger>
            <TabsTrigger value="privacy">Privacy</TabsTrigger>
            <TabsTrigger value="security">Security</TabsTrigger>
            <TabsTrigger value="account">Account</TabsTrigger>
          </TabsList>

          <TabsContent value="profile"><ProfileTab /></TabsContent>
          <TabsContent value="appearance"><AppearanceTab /></TabsContent>
          <TabsContent value="notifications"><NotificationsTab /></TabsContent>
          <TabsContent value="privacy"><PrivacyTab /></TabsContent>
          <TabsContent value="security"><SecurityTab /></TabsContent>
          <TabsContent value="account"><AccountTab /></TabsContent>
        </Tabs>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Profile
// ---------------------------------------------------------------------------

function ProfileTab() {
  const { data: me } = useMe();
  const queryClient = useQueryClient();
  const router = useRouter();
  const [displayName, setDisplayName] = React.useState("");
  const [username, setUsername] = React.useState("");
  const [about, setAbout] = React.useState("");
  const [phone, setPhone] = React.useState("");
  const [busy, setBusy] = React.useState(false);
  const [uploading, setUploading] = React.useState(false);
  const fileRef = React.useRef<HTMLInputElement>(null);
  const initialized = React.useRef(false);

  React.useEffect(() => {
    if (me && !initialized.current) {
      initialized.current = true;
      setDisplayName(me.display_name);
      setUsername(me.username);
      setAbout(me.about ?? "");
      setPhone(me.phone?.number ?? "");
    }
  }, [me]);

  if (!me) return null;

  const usernameChanged = username !== me.username;
  const usernameValid = /^[a-z0-9_]{3,24}$/.test(username);

  async function save() {
    setBusy(true);
    try {
      const updates: Record<string, string> = {};
      if (displayName !== me!.display_name) updates.display_name = displayName;
      if (username !== me!.username) updates.username = username;
      if (about !== (me!.about ?? "")) updates.about = about;
      if (Object.keys(updates).length > 0) {
        await api.updateMe(updates);
      }
      const currentPhone = me!.phone?.number ?? "";
      if (phone.trim() !== currentPhone) {
        await api.setPhone(phone.trim());
      }
      await queryClient.invalidateQueries({ queryKey: ["me"] });
      toast.success("Profile updated");
      if (usernameChanged) router.refresh();
    } catch (err) {
      if (err instanceof ApiError) {
        toast.error(err.code === "username_exists" ? "That username is taken." : err.message);
      } else {
        toast.error("Could not save profile");
      }
    } finally {
      setBusy(false);
    }
  }

  async function uploadAvatar(file: File) {
    setUploading(true);
    try {
      await new Promise<void>((resolve, reject) => {
        const xhr = new XMLHttpRequest();
        xhr.open("POST", `${process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080"}/v1/users/me/avatar`);
        xhr.withCredentials = true;
        xhr.onload = () => {
          if (xhr.status >= 200 && xhr.status < 300) resolve();
          else {
            try {
              const j = JSON.parse(xhr.responseText);
              reject(new ApiError(xhr.status, j?.error?.code ?? "error", j?.error?.message ?? "Upload failed"));
            } catch {
              reject(new ApiError(xhr.status, "error", `Upload failed (${xhr.status})`));
            }
          }
        };
        xhr.onerror = () => reject(new ApiError(0, "network_error", "Network error"));
        const fd = new FormData();
        fd.append("file", file);
        xhr.send(fd);
      });
      await queryClient.invalidateQueries({ queryKey: ["me"] });
      toast.success("Photo updated");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Upload failed");
    } finally {
      setUploading(false);
    }
  }

  return (
    <div className="mt-4 space-y-6 rounded-2xl bg-surface p-6 shadow-sm">
      {/* Avatar */}
      <div className="flex items-center gap-5">
        <div className="relative">
          <Avatar src={me.avatar_url} name={me.display_name} userId={me.id} size={72} />
          <button
            onClick={() => fileRef.current?.click()}
            className="absolute -bottom-1 -right-1 flex h-7 w-7 items-center justify-center rounded-full bg-primary text-primary-foreground shadow-md hover:bg-primary-hover"
            aria-label="Change photo"
          >
            <Camera size={13} />
          </button>
          <input
            ref={fileRef}
            type="file"
            accept="image/*"
            hidden
            onChange={(e) => e.target.files?.[0] && uploadAvatar(e.target.files[0])}
          />
        </div>
        <div>
          <p className="text-[15px] font-semibold">{me.display_name}</p>
          <p className="text-xs text-muted">@{me.username}</p>
          <p className="mt-1 flex items-center gap-1 text-[11px] text-muted">
            <BadgeCheck size={12} className={me.email_verified ? "text-success" : "text-warning"} />
            {me.email_verified ? "Email verified" : "Email not verified"}
          </p>
        </div>
        {uploading && <span className="text-xs text-muted">Uploading…</span>}
      </div>

      <div className="grid gap-4 sm:grid-cols-2">
        <div>
          <Label htmlFor="displayName">Display name</Label>
          <Input id="displayName" className="mt-1.5" value={displayName} onChange={(e) => setDisplayName(e.target.value)} maxLength={80} />
        </div>
        <div>
          <Label htmlFor="username">Username</Label>
          <div className="relative mt-1.5">
            <span className="absolute left-3.5 top-1/2 -translate-y-1/2 text-muted">@</span>
            <Input
              id="username"
              className="pl-9"
              value={username}
              onChange={(e) => setUsername(e.target.value.toLowerCase().replace(/[^a-z0-9_]/g, ""))}
              maxLength={24}
            />
          </div>
          {usernameChanged && !usernameValid && (
            <p className="mt-1 text-xs text-danger">3-24 characters: lowercase letters, numbers, underscores.</p>
          )}
        </div>
      </div>

      <div>
        <Label htmlFor="about">About</Label>
        <Textarea id="about" className="mt-1.5" rows={3} value={about} onChange={(e) => setAbout(e.target.value)} maxLength={280} placeholder="A few words about you" />
        <p className="mt-1 text-right text-[11px] text-muted">{about.length}/280</p>
      </div>

      <div>
        <Label htmlFor="phone">Mobile number (optional)</Label>
        <Input id="phone" className="mt-1.5" placeholder="+15551234567" value={phone} onChange={(e) => setPhone(e.target.value)} />
        <p className="mt-1 text-[11px] text-muted">
          Numbers stay unverified in the local build (no SMS provider), so phone discovery stays disabled.
        </p>
      </div>

      <div className="flex justify-end">
        <Button onClick={save} loading={busy}>Save changes</Button>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Appearance
// ---------------------------------------------------------------------------

function AppearanceTab() {
  const [theme, setTheme] = React.useState("system");

  React.useEffect(() => {
    setTheme(localStorage.getItem("veylo-theme") ?? "system");
  }, []);

  function apply(t: string) {
    localStorage.setItem("veylo-theme", t);
    const dark = t === "dark" || (t === "system" && window.matchMedia("(prefers-color-scheme: dark)").matches);
    document.documentElement.classList.toggle("dark", dark);
    setTheme(t);
  }

  return (
    <div className="mt-4 rounded-2xl bg-surface p-6 shadow-sm">
      <div className="flex items-center gap-2.5">
        <Palette size={18} className="text-primary" />
        <h3 className="text-[15px] font-semibold">Theme</h3>
      </div>
      <div className="mt-4 grid grid-cols-3 gap-3">
        {[
          { key: "light", label: "Light", preview: "#f7f7fa", bg: "#ffffff" },
          { key: "dark", label: "Dark", preview: "#0e0d13", bg: "#17161d" },
          { key: "system", label: "System", preview: "linear-gradient(135deg,#f7f7fa 50%,#0e0d13 50%)", bg: "#e8e8ef" },
        ].map((opt) => (
          <button
            key={opt.key}
            onClick={() => apply(opt.key)}
            className={cn(
              "rounded-2xl border-2 p-3 text-center transition-all",
              theme === opt.key ? "border-primary" : "border-transparent hover:border-border",
            )}
          >
            <span
              className="mx-auto block h-16 w-full rounded-xl border border-border shadow-inner"
              style={{ background: opt.preview }}
            />
            <span className="mt-2 block text-xs font-medium">{opt.label}</span>
          </button>
        ))}
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Notifications
// ---------------------------------------------------------------------------

function NotificationsTab() {
  const { data: settings } = useNotificationSettings();
  const queryClient = useQueryClient();

  if (!settings) return null;

  async function update(p: Partial<NotificationSettings>) {
    await api.updateNotificationSettings(p);
    await queryClient.invalidateQueries({ queryKey: ["notifSettings"] });
  }

  const rows: { key: keyof NotificationSettings; label: string; hint: string }[] = [
    { key: "notify_messages", label: "Direct messages", hint: "New messages in direct conversations" },
    { key: "notify_mentions", label: "Mentions", hint: "When someone @mentions you" },
    { key: "notify_group_invites", label: "Group activity", hint: "Invites and additions to groups" },
    { key: "notify_contact_requests", label: "Contact requests", hint: "New requests and acceptances" },
  ];

  return (
    <div className="mt-4 rounded-2xl bg-surface p-6 shadow-sm">
      <div className="flex items-center gap-2.5">
        <Bell size={18} className="text-primary" />
        <h3 className="text-[15px] font-semibold">In-app notifications</h3>
      </div>
      <ul className="mt-4 divide-y divide-border">
        {rows.map((r) => (
          <li key={r.key} className="flex items-center justify-between gap-4 py-3.5">
            <div>
              <p className="text-sm font-medium">{r.label}</p>
              <p className="text-xs text-muted">{r.hint}</p>
            </div>
            <Switch
              checked={settings[r.key] as boolean}
              onCheckedChange={(v) => update({ [r.key]: v } as Partial<NotificationSettings>)}
            />
          </li>
        ))}
      </ul>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Privacy
// ---------------------------------------------------------------------------

function PrivacyTab() {
  const { data: settings } = usePrivacySettings();
  const queryClient = useQueryClient();

  if (!settings) return null;

  async function update(p: Partial<PrivacySettings>) {
    await api.updatePrivacySettings(p);
    await queryClient.invalidateQueries({ queryKey: ["privacy"] });
  }

  const visibilityOptions = ["everyone", "contacts", "nobody"] as const;

  return (
    <div className="mt-4 space-y-4">
      <div className="rounded-2xl bg-surface p-6 shadow-sm">
        <div className="flex items-center gap-2.5">
          <ShieldCheck size={18} className="text-primary" />
          <h3 className="text-[15px] font-semibold">Visibility</h3>
        </div>

        <div className="mt-4 space-y-5">
          <div>
            <Label>Last seen</Label>
            <div className="mt-2 flex gap-2">
              {visibilityOptions.map((v) => (
                <button
                  key={v}
                  onClick={() => update({ last_seen_visibility: v })}
                  className={cn(
                    "rounded-xl px-3 py-1.5 text-xs font-medium capitalize transition-colors",
                    settings.last_seen_visibility === v
                      ? "bg-primary text-primary-foreground"
                      : "bg-surface-2 text-muted hover:text-foreground",
                  )}
                >
                  {v}
                </button>
              ))}
            </div>
          </div>
          <div>
            <Label>Online status</Label>
            <div className="mt-2 flex gap-2">
              {visibilityOptions.map((v) => (
                <button
                  key={v}
                  onClick={() => update({ online_visibility: v })}
                  className={cn(
                    "rounded-xl px-3 py-1.5 text-xs font-medium capitalize transition-colors",
                    settings.online_visibility === v
                      ? "bg-primary text-primary-foreground"
                      : "bg-surface-2 text-muted hover:text-foreground",
                  )}
                >
                  {v}
                </button>
              ))}
            </div>
          </div>
        </div>
      </div>

      <div className="rounded-2xl bg-surface p-6 shadow-sm">
        <ul className="divide-y divide-border">
          <li className="flex items-center justify-between gap-4 py-3.5">
            <div>
              <p className="text-sm font-medium">Read receipts</p>
              <p className="text-xs text-muted">Let people see when you read their messages</p>
            </div>
            <Switch
              checked={settings.read_receipts_enabled}
              onCheckedChange={(v) => update({ read_receipts_enabled: v })}
            />
          </li>
          <li className="flex items-center justify-between gap-4 py-3.5">
            <div>
              <p className="text-sm font-medium">Discoverable by email</p>
              <p className="text-xs text-muted">People who know your email can find you</p>
            </div>
            <Switch
              checked={settings.discoverable_by_email}
              onCheckedChange={(v) => update({ discoverable_by_email: v })}
            />
          </li>
          <li className="flex items-center justify-between gap-4 py-3.5">
            <div>
              <p className="text-sm font-medium">Discoverable by phone</p>
              <p className="text-xs text-muted">Only verified numbers are ever searchable</p>
            </div>
            <Switch
              checked={settings.discoverable_by_phone}
              onCheckedChange={(v) => update({ discoverable_by_phone: v })}
            />
          </li>
        </ul>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Security (sessions + password)
// ---------------------------------------------------------------------------

function SecurityTab() {
  const { data: sessions } = useSessions();

  async function revoke(id: string) {
    await api.revokeSession(id);
    toast.success("Session revoked");
  }

  async function revokeOthers() {
    await api.revokeOtherSessions();
    toast.success("All other sessions signed out");
  }

  return (
    <div className="mt-4 space-y-4">
      <div className="rounded-2xl bg-surface p-6 shadow-sm">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2.5">
            <MonitorSmartphone size={18} className="text-primary" />
            <h3 className="text-[15px] font-semibold">Active sessions</h3>
          </div>
          <Button size="sm" variant="outline" onClick={revokeOthers}>
            <LogOut size={13} /> Sign out others
          </Button>
        </div>
        <ul className="mt-4 divide-y divide-border">
          {(sessions ?? []).map((s: SessionInfo) => (
            <li key={s.id} className="flex items-center justify-between gap-4 py-3.5">
              <div className="min-w-0">
                <p className="flex items-center gap-2 text-sm font-medium">
                  {s.device_label}
                  {s.current && (
                    <span className="rounded-full bg-success/15 px-2 py-0.5 text-[10px] font-semibold text-success">
                      This device
                    </span>
                  )}
                </p>
                <p className="text-xs text-muted">
                  Active {formatRelative(s.last_active_at)} · {s.ip_address || "local"}
                </p>
              </div>
              {!s.current && (
                <Button size="sm" variant="ghost" onClick={() => revoke(s.id)}>
                  Revoke
                </Button>
              )}
            </li>
          ))}
        </ul>
      </div>

      <ChangePasswordCard />
    </div>
  );
}

function ChangePasswordCard() {
  const [current, setCurrent] = React.useState("");
  const [next, setNext] = React.useState("");
  const [busy, setBusy] = React.useState(false);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    try {
      await api.changePassword(current, next);
      toast.success("Password changed");
      setCurrent("");
      setNext("");
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "Could not change password");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="rounded-2xl bg-surface p-6 shadow-sm">
      <div className="flex items-center gap-2.5">
        <Lock size={18} className="text-primary" />
        <h3 className="text-[15px] font-semibold">Change password</h3>
      </div>
      <form onSubmit={submit} className="mt-4 grid gap-4 sm:grid-cols-2">
        <div>
          <Label htmlFor="curPass">Current password</Label>
          <Input id="curPass" type="password" className="mt-1.5" value={current} onChange={(e) => setCurrent(e.target.value)} required autoComplete="current-password" />
        </div>
        <div>
          <Label htmlFor="newPass">New password</Label>
          <Input id="newPass" type="password" className="mt-1.5" value={next} onChange={(e) => setNext(e.target.value)} required autoComplete="new-password" />
        </div>
        <div className="sm:col-span-2 flex justify-end">
          <Button type="submit" loading={busy} disabled={next.length < 8}>Update password</Button>
        </div>
      </form>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Account
// ---------------------------------------------------------------------------

function AccountTab() {
  const { data: me } = useMe();
  const router = useRouter();

  if (!me) return null;

  return (
    <div className="mt-4 space-y-4">
      <div className="rounded-2xl bg-surface p-6 shadow-sm">
        <div className="flex items-center gap-2.5">
          <User size={18} className="text-primary" />
          <h3 className="text-[15px] font-semibold">Account</h3>
        </div>
        <dl className="mt-4 space-y-3 text-sm">
          <div className="flex justify-between">
            <dt className="text-muted">Email</dt>
            <dd className="font-medium">{me.email}</dd>
          </div>
          <div className="flex justify-between">
            <dt className="text-muted">Member since</dt>
            <dd className="font-medium">{new Date(me.created_at).toLocaleDateString()}</dd>
          </div>
          <div className="flex justify-between">
            <dt className="text-muted">Email verification</dt>
            <dd className={me.email_verified ? "font-medium text-success" : "font-medium text-warning"}>
              {me.email_verified ? "Verified" : "Pending"}
            </dd>
          </div>
        </dl>
      </div>

      <div className="rounded-2xl border border-danger/30 bg-danger/5 p-6">
        <h3 className="text-[15px] font-semibold text-danger">Danger zone</h3>
        <p className="mt-1 text-xs text-muted">
          Signing out revokes this session on this device. Resetting your password signs out
          everywhere. Account deletion is not available in the local build.
        </p>
        <div className="mt-4 flex gap-2">
          <Button
            variant="outline"
            onClick={async () => {
              await api.logout();
              realtime.dispose();
              router.push("/login");
              router.refresh();
            }}
          >
            <LogOut size={14} /> Sign out
          </Button>
        </div>
      </div>
    </div>
  );
}
