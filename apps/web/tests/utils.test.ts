import { describe, it, expect } from "vitest";
import { initials, avatarGradient, formatBytes, formatRelative, newClientMessageId, cn } from "@/lib/utils";

describe("utils", () => {
  it("derives initials from names", () => {
    expect(initials("Alice Morgan")).toBe("AM");
    expect(initials("bob")).toBe("B");
    expect(initials("  ")).toBe("?");
  });

  it("builds a stable gradient per id", () => {
    expect(avatarGradient("abc")).toBe(avatarGradient("abc"));
    expect(avatarGradient("abc")).not.toBe(avatarGradient("xyz"));
  });

  it("formats byte sizes", () => {
    expect(formatBytes(500)).toBe("500 B");
    expect(formatBytes(2048)).toBe("2.0 KB");
    expect(formatBytes(3 * 1024 * 1024)).toBe("3.0 MB");
  });

  it("formats relative time buckets", () => {
    const now = Date.now();
    expect(formatRelative(new Date(now - 30_000).toISOString())).toBe("just now");
    expect(formatRelative(new Date(now - 5 * 60_000).toISOString())).toBe("5m ago");
    expect(formatRelative(new Date(now - 3 * 3600_000).toISOString())).toBe("3h ago");
    expect(formatRelative(new Date(now - 2 * 86400_000).toISOString())).toBe("2d ago");
  });

  it("generates unique client message ids", () => {
    const a = newClientMessageId();
    const b = newClientMessageId();
    expect(a).not.toBe(b);
    expect(a).toMatch(/^[0-9a-f-]{36}$/);
  });

  it("merges class names", () => {
    expect(cn("a", false && "b", "c")).toBe("a c");
  });
});
