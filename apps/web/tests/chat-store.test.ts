import { describe, it, expect, beforeEach } from "vitest";
import { useChatStore } from "@/stores/chat";

const fresh = () => {
  useChatStore.setState({
    connection: "idle",
    presence: {},
    typing: {},
    optimistic: {},
  });
};

describe("chat store", () => {
  beforeEach(fresh);

  it("tracks connection state", () => {
    useChatStore.getState().setConnection("connected");
    expect(useChatStore.getState().connection).toBe("connected");
  });

  it("tracks presence per user", () => {
    const s = useChatStore.getState();
    s.setPresence("u1", true);
    s.setPresence("u2", false);
    expect(useChatStore.getState().presence).toEqual({ u1: true, u2: false });
    s.setPresence("u1", false);
    expect(useChatStore.getState().presence["u1"]).toBe(false);
  });

  it("marks and clears typing without duplicates", () => {
    const s = useChatStore.getState();
    s.markTyping("c1", "u1", true);
    s.markTyping("c1", "u1", true); // idempotent
    expect(useChatStore.getState().typing["c1"]).toEqual(["u1"]);
    s.markTyping("c1", "u2", true);
    expect(useChatStore.getState().typing["c1"]).toEqual(["u1", "u2"]);
    s.markTyping("c1", "u1", false);
    expect(useChatStore.getState().typing["c1"]).toEqual(["u2"]);
  });

  it("manages optimistic message lifecycle", () => {
    const s = useChatStore.getState();
    const base = {
      id: "pending-1",
      conversation_id: "c1",
      sender: null,
      type: "text" as const,
      text: "hello",
      seq: Number.MAX_SAFE_INTEGER,
      reply_to: null,
      attachments: null,
      reactions: null,
      created_at: new Date().toISOString(),
      edited_at: null,
      deleted: false,
      is_mine: true,
      receipts: null,
      client_message_id: "client-1",
      pending: "sending" as const,
    };
    s.addOptimistic("c1", base);
    expect(useChatStore.getState().optimistic["c1"]).toHaveLength(1);

    s.resolveOptimistic("c1", "client-1", "server-1");
    expect(useChatStore.getState().optimistic["c1"]).toHaveLength(0);

    s.addOptimistic("c1", base);
    s.failOptimistic("c1", "client-1");
    expect(useChatStore.getState().optimistic["c1"][0].pending).toBe("failed");

    s.removeOptimistic("c1", "client-1");
    expect(useChatStore.getState().optimistic["c1"]).toHaveLength(0);
  });
});
