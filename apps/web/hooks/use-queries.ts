"use client";

// Thin TanStack Query wrappers over the API client.

import { useMutation, useQuery, useInfiniteQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import type { Message, MessagesPage } from "@/lib/types";

export function useMe() {
  return useQuery({ queryKey: ["me"], queryFn: () => api.me() });
}

export function useConversations() {
  return useQuery({ queryKey: ["conversations"], queryFn: () => api.conversations() });
}

export function useConversation(id: string | null) {
  return useQuery({
    queryKey: ["conversation", id],
    queryFn: () => api.conversation(id!),
    enabled: !!id,
  });
}

export function useMessages(conversationId: string | null) {
  return useInfiniteMessages(conversationId);
}

export function useInfiniteMessages(conversationId: string | null) {
  return useInfiniteQuery({
    queryKey: ["messages", conversationId],
    enabled: !!conversationId,
    queryFn: async ({ pageParam }) => {
      if (pageParam !== null) {
        return api.messages(conversationId!, { limit: 40, before_seq: pageParam });
      }
      return api.messages(conversationId!, { limit: 40 });
    },
    initialPageParam: null as number | null,
    getNextPageParam: () => undefined,
    getPreviousPageParam: (firstPage: MessagesPage) =>
      firstPage.has_more && firstPage.messages.length > 0
        ? firstPage.messages[0].seq
        : undefined,
    maxPages: 100,
  });
}

export function useSearchUsers(q: string, enabled = true) {
  return useQuery({
    queryKey: ["userSearch", q],
    queryFn: () => api.searchUsers(q),
    enabled: enabled && q.trim().length > 0,
    staleTime: 5_000,
  });
}

export function useContacts() {
  return useQuery({ queryKey: ["contacts"], queryFn: () => api.contacts() });
}

export function useContactRequests(direction: "incoming" | "outgoing") {
  return useQuery({
    queryKey: ["contactRequests", direction],
    queryFn: () => api.contactRequests(direction),
  });
}

export function useBlocks() {
  return useQuery({ queryKey: ["blocks"], queryFn: () => api.blocks() });
}

export function useNotifications() {
  return useQuery({
    queryKey: ["notifications"],
    queryFn: () => api.notifications(),
    refetchInterval: 30_000,
  });
}

export function useSessions() {
  return useQuery({ queryKey: ["sessions"], queryFn: () => api.sessions() });
}

export function usePrivacySettings() {
  return useQuery({ queryKey: ["privacy"], queryFn: () => api.privacySettings() });
}

export function useNotificationSettings() {
  return useQuery({ queryKey: ["notifSettings"], queryFn: () => api.notificationSettings() });
}

export function useGallery(conversationId: string | null) {
  return useQuery({
    queryKey: ["gallery", conversationId],
    queryFn: () => api.gallery(conversationId!),
    enabled: !!conversationId,
  });
}

export function useMessageReceipts(messageId: string | null) {
  return useQuery({
    queryKey: ["receipts", messageId],
    queryFn: () => api.messageReceipts(messageId!),
    enabled: !!messageId,
  });
}

// Mutation helper that invalidates the conversation list afterwards.
export function useConversationMutation<TVars>(
  fn: (vars: TVars) => Promise<unknown>,
  opts: { invalidate?: string[] } = {},
) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: fn,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["conversations"] });
      for (const key of opts.invalidate ?? []) {
        queryClient.invalidateQueries({ queryKey: [key] });
      }
    },
  });
}

// Flatten an infinite messages query into a single ordered list.
export function flattenMessages(pages: { messages: Message[] }[] | undefined): Message[] {
  if (!pages) return [];
  const seen = new Set<string>();
  const out: Message[] = [];
  for (const page of pages) {
    for (const m of page.messages) {
      if (seen.has(m.id)) continue;
      seen.add(m.id);
      out.push(m);
    }
  }
  return out;
}
