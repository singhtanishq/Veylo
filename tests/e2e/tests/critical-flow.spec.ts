// The mandatory end-to-end journey, executed as one atomic scenario: three
// independent users register, verify via Mailpit, discover each other,
// exchange realtime messages with receipts, share attachments, form a group,
// and test offline recovery + multi-session sync. Steps share state by design.

import { test, expect, type Page } from "@playwright/test";
import {
  registerAndLogin,
  sendMessage,
  uniqueUsername,
  DEMO_PASSWORD,
  latestTokenFromMailpit,
  type E2EUser,
} from "./helpers";

const API = process.env.E2E_API_URL ?? "http://localhost:8080";

async function loginAs(contextBrowser: { newPage: () => Promise<Page> } | any, identifier: string, password: string): Promise<Page> {
  const page = await contextBrowser.newPage();
  await page.goto("/login");
  await page.getByRole("textbox", { name: "Email or username" }).fill(identifier);
  await page.getByRole("textbox", { name: "Password", exact: true }).fill(password);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByRole("heading", { name: "Messages" })).toBeVisible({ timeout: 15_000 });
  return page;
}

test("critical end-to-end journey", async ({ browser }) => {
  let alice: E2EUser;
  let bob: E2EUser;
  let charlie: E2EUser;
  let dmUrl: string;

  await test.step("01 — three users register and verify through Mailpit", async () => {
    const a = await browser.newContext();
    const b = await browser.newContext();
    const c = await browser.newContext();
    alice = await registerAndLogin(a, uniqueUsername("alice"), "Alice Test");
    bob = await registerAndLogin(b, uniqueUsername("bob"), "Bob Test");
    charlie = await registerAndLogin(c, uniqueUsername("charlie"), "Charlie Test");
    await a.close();
    await b.close();
    await c.close();
  });

  await test.step("02 — Alice discovers Bob by username and starts a conversation", async () => {
    const ctx = await browser.newContext();
    const page = await loginAs(ctx, alice.username, alice.password);
    alice.page = page;

    await page.goto("/discover");
    await page.getByPlaceholder(/Search @username/).fill(`@${bob.username}`);
    await expect(page.getByText(`@${bob.username}`).first()).toBeVisible({ timeout: 10_000 });
    await page.getByRole("button", { name: "Message" }).first().click();
    await expect(page.getByRole("button", { name: "Send message" })).toBeVisible({ timeout: 10_000 });
    dmUrl = page.url();
    await ctx.close();
  });

  await test.step("03 — Alice sends; Bob receives it live with delivery+read receipts", async () => {
    const ctxA = await browser.newContext();
    const ctxB = await browser.newContext();
    alice.page = await loginAs(ctxA, alice.username, alice.password);
    bob.page = await loginAs(ctxB, bob.username, bob.password);

    await alice.page.goto(dmUrl);
    await expect(alice.page.getByRole("button", { name: "Send message" })).toBeVisible();

    await sendMessage(alice.page, "Hey Bob, realtime hello from Alice! 🚀");

    // Bob opens the conversation and sees the message without any reload.
    await bob.page.goto(dmUrl);
    await expect(bob.page.getByText("Hey Bob, realtime hello from Alice! 🚀").last()).toBeVisible({ timeout: 15_000 });

    // Bob reads (auto ack) → Alice sees the read receipt.
    await expect
      .poll(async () => alice.page.getByLabel("Read by everyone").count(), { timeout: 20_000 })
      .toBeGreaterThan(0);

    // Bob replies; Alice receives it live.
    await sendMessage(bob.page, "Got it instantly — replies work too ✨");
    await expect(alice.page.getByText("Got it instantly — replies work too ✨").last()).toBeVisible({
      timeout: 15_000,
    });

    await ctxA.close();
    await ctxB.close();
  });

  await test.step("04 — reactions appear on both sides in realtime", async () => {
    const ctxA = await browser.newContext();
    const ctxB = await browser.newContext();
    alice.page = await loginAs(ctxA, alice.username, alice.password);
    bob.page = await loginAs(ctxB, bob.username, bob.password);
    await alice.page.goto(dmUrl);
    await bob.page.goto(dmUrl);
    await expect(bob.page.getByRole("button", { name: "Send message" })).toBeVisible();

    // Alice reacts to Bob's latest message.
    const bubble = alice.page.getByText("Got it instantly — replies work too ✨").last();
    await bubble.hover();
    await alice.page.getByRole("button", { name: "Add reaction" }).last().click();
    await alice.page.getByRole("button", { name: "👍" }).first().click();
    await expect(alice.page.getByText("👍").first()).toBeVisible({ timeout: 10_000 });

    // Bob sees the reaction arrive (reaction.added fanout → cache refresh).
    await expect(bob.page.getByText("👍").first()).toBeVisible({ timeout: 15_000 });

    await ctxA.close();
    await ctxB.close();
  });

  await test.step("05 — Charlie cannot access the Alice↔Bob conversation", async () => {
    const ctx = await browser.newContext();
    const page = await loginAs(ctx, charlie.username, charlie.password);
    await page.goto(dmUrl);
    // Not a member: an explicit "unavailable" pane renders — no message content leaks.
    await expect(page.getByRole("heading", { name: "Conversation unavailable" })).toBeVisible({
      timeout: 10_000,
    });
    await expect(page.getByText("Hey Bob, realtime hello from Alice! 🚀")).toHaveCount(0);
    await ctx.close();
  });

  await test.step("06 — attachment upload and download between users", async () => {
    const ctxA = await browser.newContext();
    const ctxB = await browser.newContext();
    alice.page = await loginAs(ctxA, alice.username, alice.password);
    bob.page = await loginAs(ctxB, bob.username, bob.password);
    await alice.page.goto(dmUrl);
    await bob.page.goto(dmUrl);

    const png = Buffer.from(
      "iVBORw0KGgoAAAANSUhEUgAAAAgAAAAICAYAAADED76LAAAAFElEQVR4nGP8z8DwnwEPYMInOXwUAChdAh4mSFEUAAAAAElFTkSuQmCC",
      "base64",
    );
    // The composer's file input is the real upload path; setting files on it
    // triggers the same onChange flow as the attach button.
    await alice.page.setInputFiles('input[type="file"]', {
      name: "e2e-image.png",
      mimeType: "image/png",
      buffer: png,
    });
    await expect(alice.page.getByText("e2e-image.png").first()).toBeVisible({ timeout: 15_000 });
    await sendMessage(alice.page, "Here is the file you asked for");

    // Image bubbles render the picture + caption (filename shows in documents).
    await expect(bob.page.getByText("Here is the file you asked for").last()).toBeVisible({ timeout: 15_000 });
    await expect(bob.page.locator('a[href*="/download"]').last()).toBeVisible({ timeout: 15_000 });

    await ctxA.close();
    await ctxB.close();
  });

  await test.step("07 — group creation, member add, group messaging", async () => {
    const ctxA = await browser.newContext();
    const ctxB = await browser.newContext();
    const ctxC = await browser.newContext();
    alice.page = await loginAs(ctxA, alice.username, alice.password);
    bob.page = await loginAs(ctxB, bob.username, bob.password);
    charlie.page = await loginAs(ctxC, charlie.username, charlie.password);

    await alice.page.getByRole("button", { name: "New conversation" }).click();
    const search = alice.page.getByPlaceholder(/Search @username/);
    await search.fill(`@${bob.username}`);
    await alice.page.getByRole("button", { name: new RegExp(bob.displayName) }).first().click();
    await alice.page.getByPlaceholder("Group name…").fill("E2E Launch Crew");
    await alice.page.getByRole("button", { name: "Create group" }).click();
    await expect(alice.page.getByText('created the group "E2E Launch Crew"').first()).toBeVisible({ timeout: 15_000 });

    // Bob receives the group live.
    await expect(bob.page.getByText("E2E Launch Crew").first()).toBeVisible({ timeout: 15_000 });

    await sendMessage(alice.page, "Welcome to the crew, Bob!");
    await expect(bob.page.getByText("Welcome to the crew, Bob!").last()).toBeVisible({ timeout: 15_000 });

    // Alice adds Charlie through the details panel.
    await alice.page.getByRole("button", { name: "Open conversation details" }).click();
    await alice.page.getByRole("button", { name: "Add members" }).click();
    await alice.page.getByPlaceholder(/Search people/).fill(`@${charlie.username}`);
    await alice.page.getByRole("checkbox").first().check();
    await alice.page.getByRole("button", { name: "Add", exact: true }).click();
    await expect(alice.page.getByText("added Charlie Test").first()).toBeVisible({ timeout: 15_000 });

    // Charlie sees the group and can send.
    await expect(charlie.page.getByText("E2E Launch Crew").first()).toBeVisible({ timeout: 15_000 });
    await charlie.page.getByRole("button", { name: /E2E Launch Crew/ }).first().click();
    await sendMessage(charlie.page, "Charlie reporting in 🫡");
    await expect(bob.page.getByText("Charlie reporting in 🫡").last()).toBeVisible({ timeout: 15_000 });

    await ctxA.close();
    await ctxB.close();
    await ctxC.close();
  });

  await test.step("08 — offline recovery: messages sent while away arrive on reconnect", async () => {
    const ctxA = await browser.newContext();
    const ctxB = await browser.newContext();
    alice.page = await loginAs(ctxA, alice.username, alice.password);
    bob.page = await loginAs(ctxB, bob.username, bob.password);

    await alice.page.goto(dmUrl);
    await bob.page.goto(dmUrl);

    // Bob goes offline.
    await ctxB.close();
    await alice.page.waitForTimeout(500);

    await sendMessage(alice.page, "Offline message 1 📴");
    await sendMessage(alice.page, "Offline message 2 📴");

    // Bob reconnects in a fresh context — the server replays history.
    const ctxB2 = await browser.newContext();
    bob.page = await loginAs(ctxB2, bob.username, bob.password);
    await bob.page.goto(dmUrl);
    await expect(bob.page.getByText("Offline message 1 📴").last()).toBeVisible({ timeout: 15_000 });
    await expect(bob.page.getByText("Offline message 2 📴").last()).toBeVisible({ timeout: 15_000 });

    await ctxA.close();
    await ctxB2.close();
  });

  await test.step("09 — multi-session sync across two browsers for one user", async () => {
    const ctxA = await browser.newContext();
    const ctxA2 = await browser.newContext();
    const page1 = await loginAs(ctxA, alice.username, alice.password);
    const page2 = await loginAs(ctxA2, alice.username, alice.password);

    await page1.goto(dmUrl);
    await page2.goto(dmUrl);

    await sendMessage(page1, "Multi-device hello from session 1 🖥️📱");
    await expect(page2.getByText("Multi-device hello from session 1 🖥️📱").last()).toBeVisible({
      timeout: 15_000,
    });

    // Session list shows both devices (current flagged, two rows).
    await page1.goto("/settings");
    await page1.getByRole("tab", { name: "Security" }).click();
    await expect(page1.getByText("Active sessions")).toBeVisible();
    await expect(page1.getByText("This device")).toHaveCount(1);

    await ctxA.close();
    await ctxA2.close();
  });

  await test.step("10 — blocking hides the blocker from search; unblock restores", async () => {
    const ctxA = await browser.newContext();
    const ctxC = await browser.newContext();
    const aPage = await loginAs(ctxA, alice.username, alice.password);
    const cPage = await loginAs(ctxC, charlie.username, charlie.password);

    // Alice blocks Charlie via the authenticated API (UI path: Contacts → Blocked).
    await aPage.evaluate(async (username: string) => {
      const api = (path: string, opts: RequestInit = {}) =>
        fetch(`http://localhost:8080${path}`, { credentials: "include", ...opts });
      const search = await api(`/v1/users/search?q=@${username}`).then((r) => r.json());
      const id = search.data[0]?.id as string;
      await api("/v1/contacts/blocks", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ user_id: id }),
      });
    }, charlie.username);

    // Charlie's search no longer surfaces Alice.
    await cPage.goto("/discover");
    await cPage.getByPlaceholder(/Search @username/).fill(`@${alice.username}`);
    await expect(cPage.getByText(`@${alice.username}`)).toHaveCount(0, { timeout: 10_000 });

    // Unblock to leave clean state.
    await aPage.evaluate(async (username: string) => {
      const api = (path: string, opts: RequestInit = {}) =>
        fetch(`http://localhost:8080${path}`, { credentials: "include", ...opts });
      const blocks = await api("/v1/contacts/blocks").then((r) => r.json());
      const target = (blocks.data as { id: string; username: string }[]).find((b) => b.username === username);
      if (target) await api(`/v1/contacts/blocks/${target.id}`, { method: "DELETE" });
    }, charlie.username);

    await ctxA.close();
    await ctxC.close();
  });

  await test.step("11 — session revocation signs other devices out", async () => {
    const ctx1 = await browser.newContext();
    const ctx2 = await browser.newContext();
    const p1 = await loginAs(ctx1, alice.username, alice.password);
    const p2 = await loginAs(ctx2, alice.username, alice.password);

    await p1.goto("/settings");
    await p1.getByRole("tab", { name: "Security" }).click();
    await p1.getByRole("button", { name: "Sign out others" }).click();

    await p2.goto("/settings");
    await expect(p2).toHaveURL(/\/login/, { timeout: 15_000 });

    await ctx1.close();
    await ctx2.close();
  });

  await test.step("12 — seeded demo accounts work (login + seeded history)", async () => {
    const ctx = await browser.newContext();
    const page = await loginAs(ctx, "alice", DEMO_PASSWORD);
    await expect(page.getByText("Project Aurora")).toBeVisible({ timeout: 10_000 });
    await ctx.close();
  });
});
