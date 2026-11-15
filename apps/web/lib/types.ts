// Shared API types mirroring the Go backend's JSON contracts.

export interface Me {
  id: string;
  username: string;
  display_name: string;
  about: string | null;
  avatar_url: string | null;
  email: string;
  email_verified: boolean;
  created_at: string;
  phone?: { number: string; verified: boolean };
}

export interface PublicUser {
  id: string;
  username: string;
  display_name: string;
  about: string | null;
  avatar_url: string | null;
  online: boolean;
  last_seen_at: string | null;
  created_at: string;
  is_contact: boolean;
  blocked_by_me?: boolean;
}

export interface ContactUser {
  id: string;
  username: string;
  display_name: string;
  about: string | null;
  avatar_url: string | null;
  online: boolean;
  last_seen_at: string | null;
  added_at?: string;
}

export interface ContactRequest {
  id: string;
  user: ContactUser;
  message: string | null;
  created_at: string;
}

export interface Member {
  id: string;
  username: string;
  display_name: string;
  about: string | null;
  avatar_url: string | null;
  role: "owner" | "admin" | "member";
  online: boolean;
  last_seen_at: string | null;
  joined_at: string;
  is_me: boolean;
}

export interface LastMessage {
  id: string;
  sender_id: string;
  sender_name: string;
  type: string;
  preview: string;
  seq: number;
  created_at: string;
}

export interface Conversation {
  id: string;
  type: "direct" | "group";
  title: string | null;
  description: string | null;
  avatar_url: string | null;
  created_at: string;
  last_message_at: string | null;
  last_message: LastMessage | null;
  unread_count: number;
  member_count: number;
  my_role: string;
  is_muted: boolean;
  is_pinned: boolean;
  is_archived: boolean;
  read_seq: number;
  other_user: Member | null;
  members: Member[];
}

export interface Sender {
  id: string;
  username: string;
  display_name: string;
  avatar_url: string | null;
}

export interface ReplyPreview {
  id: string;
  sender_name: string;
  type: string;
  preview: string;
}

export interface AttachmentMeta {
  id: string;
  file_name: string;
  mime_type: string;
  kind: "image" | "video" | "audio" | "voice" | "document";
  size_bytes: number;
  width?: number;
  height?: number;
  duration_ms?: number;
  download_url: string;
  thumbnail_url?: string;
}

export interface Reaction {
  emoji: string;
  count: number;
  user_ids: string[];
  mine: boolean;
}

export interface Receipts {
  delivered_count: number;
  read_count: number;
  total: number;
}

export interface Message {
  id: string;
  conversation_id: string;
  sender: Sender | null;
  type: "text" | "image" | "video" | "audio" | "voice" | "document" | "system";
  text: string | null;
  seq: number;
  reply_to: ReplyPreview | null;
  attachments: AttachmentMeta[] | null;
  reactions: Reaction[] | null;
  created_at: string;
  edited_at: string | null;
  deleted: boolean;
  is_mine: boolean;
  receipts: Receipts | null;
  client_message_id?: string;
  pending?: "sending" | "failed";
}

export interface MessagesPage {
  messages: Message[];
  next_cursor: string | null;
  has_more: boolean;
}

export interface Notification {
  id: string;
  type: string;
  title: string;
  body: string | null;
  conversation_id: string | null;
  message_id: string | null;
  actor: { id: string; username: string; display_name: string; avatar_url: string | null } | null;
  read_at: string | null;
  created_at: string;
}

export interface SessionInfo {
  id: string;
  device_label: string;
  device_type: string;
  ip_address: string;
  created_at: string;
  last_active_at: string;
  current: boolean;
}

export interface PrivacySettings {
  last_seen_visibility: "everyone" | "contacts" | "nobody";
  online_visibility: "everyone" | "contacts" | "nobody";
  profile_visibility: "everyone" | "contacts";
  read_receipts_enabled: boolean;
  discoverable_by_email: boolean;
  discoverable_by_phone: boolean;
  who_can_send_requests: string;
}

export interface NotificationSettings {
  notify_messages: boolean;
  notify_mentions: boolean;
  notify_contact_requests: boolean;
  notify_group_invites: boolean;
  theme: "light" | "dark" | "system";
}

export interface GalleryItem {
  message_id: string;
  message_type: string;
  created_at: string;
  sender: string;
  attachment: AttachmentMeta;
}

// ---------------------------------------------------------------------------
// WebSocket protocol
// ---------------------------------------------------------------------------

export interface WSEnvelope {
  v: number;
  type: string;
  event_id: string;
  ts?: string;
  data?: any;
}

export interface GroupInvitation {
  id: string;
  user_id: string;
  username: string;
  display_name: string;
  avatar_url: string | null;
  status: string;
  created_at: string;
}

export interface SearchHit {
  id: string;
  conversation_id: string;
  sender_id: string;
  sender_name: string;
  type: string;
  text: string;
  seq: number;
  sent_at: string;
  headline: string | null;
}

export interface ConversationHit {
  id: string;
  type: string;
  title: string | null;
  avatar_url: string | null;
  last_message_at: string | null;
}
