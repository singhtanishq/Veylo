"use client";

// Veylo realtime client. One managed WebSocket per authenticated browser
// session: ticket-based auth, exponential backoff with jitter, heartbeats,
// event-id deduplication and REST re-sync on reconnect (PostgreSQL stays the
// source of truth; the socket accelerates delivery).

import { api } from "./api";
import type { WSEnvelope } from "./types";
export type { WSEnvelope };

type Handler = (env: WSEnvelope) => void;

const MAX_BACKOFF_MS = 15000;
const BASE_BACKOFF_MS = 700;
const HEARTBEAT_MS = 25000;
const DEDUP_CACHE_SIZE = 600;

class RealtimeClient {
  private ws: WebSocket | null = null;
  private handlers = new Set<Handler>();
  private stateListeners = new Set<(state: ConnectionState) => void>();
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private heartbeatTimer: ReturnType<typeof setInterval> | null = null;
  private attempts = 0;
  private disposed = false;
  private recentEventIds: string[] = [];

  state: ConnectionState = "idle";

  setStateListener(fn: (state: ConnectionState) => void) {
    this.stateListeners.add(fn);
    fn(this.state);
    return () => this.stateListeners.delete(fn);
  }

  private setState(s: ConnectionState) {
    this.state = s;
    this.stateListeners.forEach((fn) => fn(s));
  }

  connect() {
    if (this.disposed || this.ws) return;
    this.setState("connecting");
    this.open();
  }

  private async open() {
    try {
      const { ticket } = await api.wsTicket();
      const wsUrl = wsEndpoint();
      const ws = new WebSocket(`${wsUrl}?ticket=${encodeURIComponent(ticket)}`);
      this.ws = ws;

      ws.onopen = () => {
        this.attempts = 0;
        this.setState("connected");
        this.startHeartbeat();
        // Let listeners know to re-sync missed data via REST.
        this.emit({ v: 1, type: "__resync", event_id: "resync", ts: new Date().toISOString() });
      };

      ws.onmessage = (e) => {
        try {
          const env: WSEnvelope = JSON.parse(e.data);
          if (env.type === "pong") return;
          if (env.event_id && this.seen(env.event_id)) return;
          this.emit(env);
        } catch {
          // Ignore malformed frames.
        }
      };

      ws.onclose = () => {
        this.stopHeartbeat();
        this.ws = null;
        if (!this.disposed) {
          this.setState("disconnected");
          this.scheduleReconnect();
        } else {
          this.setState("idle");
        }
      };

      ws.onerror = () => {
        // onclose follows; nothing to do here.
      };
    } catch {
      // Ticket fetch failed (offline / signed out) — retry with backoff.
      this.setState("disconnected");
      this.scheduleReconnect();
    }
  }

  private seen(id: string): boolean {
    if (this.recentEventIds.includes(id)) return true;
    this.recentEventIds.push(id);
    if (this.recentEventIds.length > DEDUP_CACHE_SIZE) {
      this.recentEventIds.splice(0, this.recentEventIds.length - DEDUP_CACHE_SIZE);
    }
    return false;
  }

  private scheduleReconnect() {
    if (this.reconnectTimer || this.disposed) return;
    const backoff = Math.min(MAX_BACKOFF_MS, BASE_BACKOFF_MS * 2 ** this.attempts);
    const jitter = backoff * (0.75 + Math.random() * 0.5);
    this.attempts += 1;
    this.reconnectTimer = setTimeout(() => {
      this.reconnectTimer = null;
      this.open();
    }, jitter);
  }

  private startHeartbeat() {
    this.stopHeartbeat();
    this.heartbeatTimer = setInterval(() => {
      if (this.ws?.readyState === WebSocket.OPEN) {
        this.ws.send(JSON.stringify({ v: 1, type: "ping", event_id: `hb-${Date.now()}` }));
      }
    }, HEARTBEAT_MS);
  }

  private stopHeartbeat() {
    if (this.heartbeatTimer) {
      clearInterval(this.heartbeatTimer);
      this.heartbeatTimer = null;
    }
  }

  private emit(env: WSEnvelope) {
    this.handlers.forEach((h) => h(env));
  }

  on(fn: Handler): () => void {
    this.handlers.add(fn);
    return () => this.handlers.delete(fn);
  }

  send(type: string, data: Record<string, unknown>): boolean {
    if (this.ws?.readyState === WebSocket.OPEN) {
      this.ws.send(
        JSON.stringify({ v: 1, type, event_id: `${type}-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`, data }),
      );
      return true;
    }
    return false;
  }

  dispose() {
    this.disposed = true;
    this.stopHeartbeat();
    if (this.reconnectTimer) clearTimeout(this.reconnectTimer);
    this.recentEventIds = [];
    if (this.ws) {
      this.ws.close(1000, "logout");
      this.ws = null;
    }
    this.handlers.clear();
    this.stateListeners.clear();
    this.setState("idle");
  }
}

export type ConnectionState = "idle" | "connecting" | "connected" | "disconnected";

function wsEndpoint(): string {
  const apiU = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";
  return apiU.replace(/^http/, "ws") + "/v1/ws";
}

// Module-level singleton: one connection per browser session.
export const realtime = new RealtimeClient();
