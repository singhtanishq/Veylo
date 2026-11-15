"use client";

// Transient realtime UI state (kept separate from server state in TanStack
// Query): connection status, presence, typing indicators and optimistic
// outgoing messages.

import { create } from "zustand";
import type { Message } from "@/lib/types";

export type OptimisticMessage = Message & { pending: "sending" | "failed" };

interface ChatState {
  connection: "idle" | "connecting" | "connected" | "disconnected";
  setConnection: (c: ChatState["connection"]) => void;

  // userID -> true if online
  presence: Record<string, boolean>;
  setPresence: (userId: string, online: boolean) => void;

  // conversationID -> userID(s) currently typing
  typing: Record<string, string[]>;
  markTyping: (conversationId: string, userId: string, isTyping: boolean) => void;

  // conversationID -> optimistic (pending/failed) messages keyed by client id
  optimistic: Record<string, OptimisticMessage[]>;
  addOptimistic: (conversationId: string, msg: OptimisticMessage) => void;
  resolveOptimistic: (conversationId: string, clientId: string, serverId: string) => void;
  failOptimistic: (conversationId: string, clientId: string) => void;
  removeOptimistic: (conversationId: string, clientId: string) => void;
  clearOptimistic: (conversationId: string) => void;

  typingSelf: Record<string, ReturnType<typeof setTimeout> | null>;
  notifyTypingSelf: (conversationId: string, sendFn: (stop: boolean) => void) => void;
}

export const useChatStore = create<ChatState>((set, get) => ({
  connection: "idle",
  setConnection: (c) => set({ connection: c }),

  presence: {},
  setPresence: (userId, online) =>
    set((s) => ({ presence: { ...s.presence, [userId]: online } })),

  typing: {},
  markTyping: (conversationId, userId, isTyping) =>
    set((s) => {
      const current = s.typing[conversationId] ?? [];
      const next = isTyping
        ? current.includes(userId)
          ? current
          : [...current, userId]
        : current.filter((id) => id !== userId);
      return { typing: { ...s.typing, [conversationId]: next } };
    }),

  optimistic: {},
  addOptimistic: (conversationId, msg) =>
    set((s) => ({
      optimistic: {
        ...s.optimistic,
        [conversationId]: [...(s.optimistic[conversationId] ?? []), msg],
      },
    })),
  resolveOptimistic: (conversationId, clientId, _serverId /* serverId reserved for dedup checks */ ) =>
    set((s) => ({
      optimistic: {
        ...s.optimistic,
        [conversationId]: (s.optimistic[conversationId] ?? []).filter(
          (m) => m.client_message_id !== clientId,
        ),
      },
    })),
  failOptimistic: (conversationId, clientId) =>
    set((s) => ({
      optimistic: {
        ...s.optimistic,
        [conversationId]: (s.optimistic[conversationId] ?? []).map((m) =>
          m.client_message_id === clientId ? { ...m, pending: "failed" as const } : m,
        ),
      },
    })),
  removeOptimistic: (conversationId, clientId) =>
    set((s) => ({
      optimistic: {
        ...s.optimistic,
        [conversationId]: (s.optimistic[conversationId] ?? []).filter(
          (m) => m.client_message_id !== clientId,
        ),
      },
    })),
  clearOptimistic: (conversationId) =>
    set((s) => {
      const next = { ...s.optimistic };
      delete next[conversationId];
      return { optimistic: next };
    }),

  typingSelf: {},
  notifyTypingSelf: (conversationId, sendFn) => {
    const timers = get().typingSelf;
    const existing = timers[conversationId];
    const start = existing === undefined || existing === null;
    if (start) sendFn(false);
    if (existing) clearTimeout(existing);
    const timer = setTimeout(() => {
      sendFn(true);
      set((s) => ({ typingSelf: { ...s.typingSelf, [conversationId]: null } }));
    }, 4000);
    set((s) => ({ typingSelf: { ...s.typingSelf, [conversationId]: timer } }));
  },
}));
