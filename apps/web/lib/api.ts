// Typed API client for the Veylo Go backend. Cookies ride along automatically
// (same-site localhost), every method unwraps the {"data": ...} envelope and
// surfaces structured API errors.

import type { Me } from "./types";

export class ApiError extends Error {
  code: string;
  status: number;
  details?: unknown;

  constructor(status: number, code: string, message: string, details?: unknown) {
    super(message);
    this.status = status;
    this.code = code;
    this.details = details;
  }
}

const API_URL =
  process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

// Server-side fetches inside Next use the internal API base.
const SERVER_API_URL = process.env.API_BASE_URL_INTERNAL ?? API_URL;

export function apiUrl(): string {
  return API_URL;
}

export function attachmentUrl(path: string | null | undefined): string | null {
  if (!path) return null;
  return `${API_URL}${path}`;
}

type Opts = {
  method?: string;
  body?: unknown;
  formData?: FormData;
  signal?: AbortSignal;
  headers?: Record<string, string>;
  serverCookie?: string; // server-component fetch: forward the session cookie
};

async function request<T = any>(method: string, path: string, opts: Opts = {}): Promise<T> {
  const base = opts.serverCookie ? SERVER_API_URL : API_URL;
  const headers: Record<string, string> = { "X-Requested-With": "veylo-web", ...opts.headers };
  let body: BodyInit | undefined;

  if (opts.formData) {
    body = opts.formData;
  } else if (opts.body !== undefined) {
    headers["Content-Type"] = "application/json";
    body = JSON.stringify(opts.body);
  }
  if (opts.serverCookie) {
    headers["Cookie"] = opts.serverCookie;
  }

  const resp = await fetch(`${base}${path}`, {
    method,
    headers,
    body,
    credentials: "include",
    signal: opts.signal,
    cache: "no-store",
  });

  const text = await resp.text();
  let json: any = {};
  if (text) {
    try {
      json = JSON.parse(text);
    } catch {
      // Non-JSON response (rare) — fall through to status handling.
    }
  }

  if (!resp.ok) {
    const err = json?.error ?? {};
    throw new ApiError(
      resp.status,
      err.code ?? "unknown_error",
      err.message ?? `Request failed (${resp.status})`,
      err.details,
    );
  }
  return (json?.data ?? json) as T;
}

// ---------------------------------------------------------------------------
// Auth
// ---------------------------------------------------------------------------

export const api = {
  register: (p: { email: string; password: string; username: string; display_name: string }) =>
    request<{ user_id: string; email: string; mailpit_url: string }>("POST", "/v1/auth/register", { body: p }),
  login: (p: { identifier: string; password: string }) =>
    request<{ user: Me; session: unknown }>("POST", "/v1/auth/login", { body: p }),
  logout: () => request("POST", "/v1/auth/logout", { body: {} }),
  verifyEmail: (token: string) =>
    request<{ verified: boolean }>("POST", "/v1/auth/verify-email", { body: { token } }),
  resendVerification: (email?: string) =>
    request<{ sent: boolean }>("POST", "/v1/auth/resend-verification", { body: { email } }),
  forgotPassword: (email: string) =>
    request<{ sent: boolean }>("POST", "/v1/auth/forgot-password", { body: { email } }),
  resetPassword: (token: string, password: string) =>
    request<{ reset: boolean }>("POST", "/v1/auth/reset-password", { body: { token, password } }),
  changePassword: (current_password: string, new_password: string) =>
    request<{ changed: boolean }>("POST", "/v1/auth/change-password", { body: { current_password, new_password } }),

  me: () => request<Me>("GET", "/v1/auth/me"),

  updateMe: (p: { display_name?: string; username?: string; about?: string }) =>
    request("PATCH", "/v1/users/me", { body: p }),
  setPhone: (phone: string) =>
    request("POST", "/v1/users/me/phone", { body: { phone } }),
  usernameAvailability: (username: string) =>
    request<{ available: boolean; reason?: string }>(
      "GET",
      `/v1/users/username/availability?username=${encodeURIComponent(username)}`,
    ),
  searchUsers: (q: string) =>
    request<import("./types").PublicUser[]>("GET", `/v1/users/search?q=${encodeURIComponent(q)}`),
  getProfile: (id: string) =>
    request<import("./types").PublicUser>("GET", `/v1/users/${id}`),

  // Contacts
  contacts: () => request<import("./types").ContactUser[]>("GET", "/v1/contacts"),
  contactRequests: (direction: "incoming" | "outgoing") =>
    request<import("./types").ContactRequest[]>("GET", `/v1/contacts/requests?direction=${direction}`),
  sendContactRequest: (userId: string) =>
    request("POST", "/v1/contacts/requests", { body: { user_id: userId } }),
  acceptContactRequest: (id: string) =>
    request("POST", `/v1/contacts/requests/${id}/accept`, { body: {} }),
  declineContactRequest: (id: string) =>
    request("POST", `/v1/contacts/requests/${id}/decline`, { body: {} }),
  removeContact: (userId: string) =>
    request("DELETE", `/v1/contacts/${userId}`),
  blocks: () => request<import("./types").ContactUser[]>("GET", "/v1/contacts/blocks"),
  block: (userId: string) => request("POST", "/v1/contacts/blocks", { body: { user_id: userId } }),
  unblock: (userId: string) => request("DELETE", `/v1/contacts/blocks/${userId}`),

  // Conversations
  conversations: () =>
    request<import("./types").Conversation[]>("GET", "/v1/conversations"),
  directConversation: (userId: string) =>
    request<import("./types").Conversation>("POST", "/v1/conversations/direct", { body: { user_id: userId } }),
  createGroup: (p: { title: string; description?: string; member_ids: string[] }) =>
    request<import("./types").Conversation>("POST", "/v1/conversations/group", { body: p }),
  conversation: (id: string) =>
    request<import("./types").Conversation>("GET", `/v1/conversations/${id}`),
  updateConversation: (id: string, p: { title?: string; description?: string }) =>
    request<import("./types").Conversation>("PATCH", `/v1/conversations/${id}`, { body: p }),
  deleteConversation: (id: string) => request("DELETE", `/v1/conversations/${id}`),
  updateMemberSettings: (
    id: string,
    p: { is_muted?: boolean; is_pinned?: boolean; is_archived?: boolean; notification_level?: string },
  ) => request<import("./types").Conversation>("PATCH", `/v1/conversations/${id}/settings`, { body: p }),
  addMembers: (id: string, userIds: string[]) =>
    request<import("./types").Conversation>("POST", `/v1/conversations/${id}/members`, { body: { user_ids: userIds } }),
  removeMember: (id: string, userId: string) =>
    request("DELETE", `/v1/conversations/${id}/members/${userId}`),
  changeRole: (id: string, userId: string, role: string) =>
    request("PATCH", `/v1/conversations/${id}/members/${userId}/role`, { body: { role } }),
  leaveConversation: (id: string) => request("DELETE", `/v1/conversations/${id}/members/me`),
  inviteToGroup: (id: string, userIds: string[]) =>
    request("POST", `/v1/conversations/${id}/invitations`, { body: { user_ids: userIds } }),
  groupInvitations: (id: string) =>
    request<import("./types").GroupInvitation[]>("GET", `/v1/conversations/${id}/invitations`),
  acceptGroupInvitation: (convId: string, invId: string) =>
    request<import("./types").Conversation>("POST", `/v1/conversations/${convId}/invitations/${invId}/accept`, { body: {} }),
  declineGroupInvitation: (convId: string, invId: string) =>
    request("POST", `/v1/conversations/${convId}/invitations/${invId}/decline`, { body: {} }),

  // Messages
  messages: (id: string, params: { limit?: number; before_seq?: number; after_seq?: number } = {}) => {
    const qs = new URLSearchParams();
    if (params.limit) qs.set("limit", String(params.limit));
    if (params.before_seq !== undefined) qs.set("before_seq", String(params.before_seq));
    if (params.after_seq !== undefined) qs.set("after_seq", String(params.after_seq));
    return request<import("./types").MessagesPage>(
      "GET",
      `/v1/conversations/${id}/messages${qs.toString() ? `?${qs}` : ""}`,
    );
  },
  sendMessage: (id: string, p: { type: string; text?: string; reply_to_id?: string; attachment_ids?: string[]; client_message_id: string }) =>
    request<import("./types").Message>("POST", `/v1/conversations/${id}/messages`, { body: p }),
  editMessage: (id: string, text: string) =>
    request<import("./types").Message>("PATCH", `/v1/messages/${id}`, { body: { text } }),
  deleteMessage: (id: string) => request("DELETE", `/v1/messages/${id}`),
  react: (id: string, emoji: string) => request("PUT", `/v1/messages/${id}/reactions`, { body: { emoji } }),
  unreact: (id: string, emoji: string) => request("DELETE", `/v1/messages/${id}/reactions`, { body: { emoji } }),
  messageReceipts: (id: string) => request("GET", `/v1/messages/${id}/receipts`),
  markRead: (id: string, maxSeq: number) =>
    request("POST", `/v1/conversations/${id}/read`, { body: { max_seq: maxSeq } }),
  gallery: (id: string, kind = "") =>
    request<import("./types").GalleryItem[]>("GET", `/v1/conversations/${id}/gallery?kind=${kind}`),

  // Attachments
  upload: (file: File, onProgress?: (pct: number) => void) =>
    new Promise<import("./types").AttachmentMeta>((resolve, reject) => {
      const xhr = new XMLHttpRequest();
      xhr.open("POST", `${API_URL}/v1/attachments`);
      xhr.withCredentials = true;
      xhr.setRequestHeader("X-Requested-With", "veylo-web");
      if (onProgress) {
        xhr.upload.onprogress = (e) => {
          if (e.lengthComputable) onProgress(Math.round((e.loaded / e.total) * 100));
        };
      }
      xhr.onload = () => {
        try {
          const json = JSON.parse(xhr.responseText);
          if (xhr.status >= 200 && xhr.status < 300) {
            resolve(json.data);
          } else {
            const err = json?.error ?? {};
            reject(new ApiError(xhr.status, err.code ?? "unknown_error", err.message ?? "Upload failed"));
          }
        } catch {
          reject(new ApiError(xhr.status, "unknown_error", `Upload failed (${xhr.status})`));
        }
      };
      xhr.onerror = () => reject(new ApiError(0, "network_error", "Network error during upload"));
      const fd = new FormData();
      fd.append("file", file);
      xhr.send(fd);
    }),
  deleteAttachment: (id: string) => request("DELETE", `/v1/attachments/${id}`),

  // Search
  searchMessages: (q: string, conversationId?: string) => {
    const qs = conversationId ? `&conversation_id=${conversationId}` : "";
    return request<import("./types").SearchHit[]>("GET", `/v1/search/messages?q=${encodeURIComponent(q)}${qs}`);
  },
  searchConversations: (q: string) =>
    request<import("./types").ConversationHit[]>("GET", `/v1/search/conversations?q=${encodeURIComponent(q)}`),

  // Notifications
  notifications: () => request<import("./types").Notification[]>("GET", "/v1/notifications"),
  markNotificationsRead: (ids?: string[], all = false) =>
    request("POST", "/v1/notifications/read", { body: all ? { all: true } : { ids } }),

  // Sessions
  sessions: () => request<import("./types").SessionInfo[]>("GET", "/v1/sessions"),
  revokeSession: (id: string) => request("DELETE", `/v1/sessions/${id}`),
  revokeOtherSessions: () => request("POST", "/v1/sessions/revoke-others", { body: {} }),

  // Settings
  privacySettings: () => request<import("./types").PrivacySettings>("GET", "/v1/settings/privacy"),
  updatePrivacySettings: (p: Partial<import("./types").PrivacySettings>) =>
    request<import("./types").PrivacySettings>("PATCH", "/v1/settings/privacy", { body: p }),
  notificationSettings: () => request<import("./types").NotificationSettings>("GET", "/v1/settings/notifications"),
  updateNotificationSettings: (p: Partial<import("./types").NotificationSettings>) =>
    request<import("./types").NotificationSettings>("PATCH", "/v1/settings/notifications", { body: p }),

  // WebSocket ticket
  wsTicket: () => request<{ ticket: string; expires_in: number }>("POST", "/v1/ws/token", { body: {} }),

  // Server-side helper (layout auth guards).
  meServer: (cookie: string) => request<Me>("GET", "/v1/auth/me", { serverCookie: cookie }),
};
