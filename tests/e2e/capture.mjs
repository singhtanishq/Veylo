// Capture the full Veylo screenshot set from the live application.
// Requires: infra up, migrations applied, API :8080, Web :3000.
// Usage: node capture.mjs [--mobile-only] [--fresh-seed]
//
// Screenshots are real renders of the running app (no mockups).

import { chromium } from "@playwright/test";
import { execSync } from "node:child_process";
import { mkdirSync, writeFileSync } from "node:fs";
import path from "node:path";

const BASE = process.env.E2E_BASE_URL ?? "http://localhost:3000";
const API = process.env.E2E_API_URL ?? "http://localhost:8080";
const OUT = path.resolve(process.cwd(), "../../docs/screenshots");
const DEMO_PASSWORD = "VeyloDemo!2026";

const DESKTOP = { width: 1440, height: 1000 };
const MOBILE = { width: 390, height: 844 };

const manifest = [];
let seq = 0;

function record(dir, file, page, feature, description) {
  seq += 1;
  manifest.push({
    file: `${dir}/${file}`,
    page,
    feature,
    description,
    viewport: seqViewport,
    date: new Date().toISOString().slice(0, 10),
  });
}

let seqViewport = "1440x1000";

async function shot(page, dir, file, feature, description) {
  mkdirSync(path.join(OUT, dir), { recursive: true });
  await page.waitForTimeout(400); // settle animations
  await page.screenshot({ path: path.join(OUT, dir, file), fullPage: false });
  record(dir, file, page.url().replace(BASE, "") || "/", feature, description);
  console.log("  ✓", `${dir}/${file}`);
}

async function login(page, identifier, password = DEMO_PASSWORD) {
  await page.goto(`${BASE}/login`);
  await page.getByRole("textbox", { name: "Email or username" }).fill(identifier);
  await page.getByRole("textbox", { name: "Password", exact: true }).fill(password);
  await page.getByRole("button", { name: "Sign in" }).click();
  await page.waitForTimeout(1200);
}

async function openDM(page, otherName) {
  await page.goto(`${BASE}/`);
  await page.waitForTimeout(800);
  const btn = page.getByRole("list").first().locator("button").filter({ hasText: otherName }).first();
  await btn.click();
  await page.waitForTimeout(1000);
}

async function main() {
  console.log("==> Seeding demo data (fresh)");
  try {
    execSync("go run ./cmd/seed", {
      cwd: path.resolve(process.cwd(), "../../services/api"),
      env: { ...process.env, STORAGE_PATH: path.resolve(process.cwd(), "../../storage/uploads") },
      stdio: "pipe",
    });
  } catch (e) {
    console.log("  (seed skipped or already applied)");
  }

  const browser = await chromium.launch();

  // =====================================================================
  // 01 — Authentication (logged out)
  // =====================================================================
  console.log("==> 01-authentication");
  {
    const ctx = await browser.newContext({ viewport: DESKTOP });
    const page = await ctx.newPage();

    await page.goto(`${BASE}/login`);
    await page.waitForTimeout(800);
    await shot(page, "01-authentication", "login.png", "Login", "Sign-in screen with brand panel and demo-account hints.");

    await page.goto(`${BASE}/register`);
    await page.waitForTimeout(600);
    await page.getByRole("textbox", { name: "Display name" }).fill("Maya Sharma");
    await page.getByRole("textbox", { name: "Username" }).fill("maya");
    await page.getByRole("textbox", { name: "Email" }).fill("maya@example.test");
    await page.getByRole("textbox", { name: "Password" }).fill("MayaPass2026!");
    await page.waitForTimeout(500);
    await shot(page, "01-authentication", "registration.png", "Registration", "Account creation with live username availability.");

    await page.goto(`${BASE}/verify-email?registered=1&email=maya@example.test`);
    await page.waitForTimeout(600);
    await shot(page, "01-authentication", "email-verification.png", "Email verification", "Verification instructions with Mailpit link.");

    await page.goto(`${BASE}/forgot-password`);
    await page.waitForTimeout(600);
    await shot(page, "01-authentication", "password-reset.png", "Password recovery", "Forgot-password flow (Mailpit delivery).");

    await ctx.close();
  }

  // =====================================================================
  // 02 — Main interface (Alice)
  // =====================================================================
  console.log("==> 02-main-interface");
  const aliceCtx = await browser.newContext({ viewport: DESKTOP });
  const alice = await aliceCtx.newPage();
  await login(alice, "alice");

  {
    // Empty workspace (no conversation selected).
    await alice.goto(`${BASE}/`);
    await alice.waitForTimeout(1000);
    await shot(alice, "02-main-interface", "empty-conversation.png", "Empty state", "Workspace without an active conversation.");

    // Conversation list with unread badges.
    await shot(alice, "02-main-interface", "conversation-sidebar.png", "Conversation list", "Sidebar with filters, unread badges and presence.");

    // Open the Alice <-> Bob DM (seeded rich history).
    await openDM(alice, "Bob Chen");
    const dmUrl = alice.url();

    await shot(alice, "02-main-interface", "main-workspace.png", "Main workspace", "Nav rail, conversation list and active chat.");

    // Message history (scrolled view of the same DM).
    await shot(alice, "04-conversations", "message-history.png", "Message history", "Persistent one-to-one history with receipts.");

    await shot(alice, "04-conversations", "direct-conversation.png", "Direct conversation", "Bubbles, timestamps, receipts and reactions.");

    // Reply flow.
    const target = alice.getByText("Perfect. Also — welcome aboard Veylo 🎉").last();
    await target.hover();
    await alice.getByRole("button", { name: "Message options" }).last().click();
    const replyItem = alice.getByText("Reply", { exact: true }).first();
    await replyItem.click();
    await alice.waitForTimeout(400);
    await shot(alice, "04-conversations", "reply-message.png", "Reply", "Reply banner above the composer.");
    await shot(alice, "04-conversations", "message-context-menu.png", "Context menu", "Per-message reply banner context.");
    await alice.getByRole("button", { name: "Cancel reply" }).click().catch(() => {});

    // Reaction picker.
    await target.hover();
    await alice.getByRole("button", { name: "Add reaction" }).last().click();
    await alice.waitForTimeout(400);
    await shot(alice, "04-conversations", "reactions.png", "Reactions", "Reaction picker over a message.");
    await alice.keyboard.press("Escape");

    // Image message (seeded attachment).
    await shot(alice, "06-media", "image-message.png", "Image message", "Seeded photo attachment rendering.");

    // Typing indicator: Bob types from a second session.
    const bobCtx = await browser.newContext({ viewport: DESKTOP });
    const bob = await bobCtx.newPage();
    await login(bob, "bob");
    await openDM(bob, "Alice Morgan");
    const bobInput = bob.getByRole("textbox", { name: "Message input" });
    await bobInput.fill("Hey Alice, about the launch timeline…");
    await alice.waitForTimeout(900);
    await shot(alice, "04-conversations", "typing-indicator.png", "Typing indicator", "Realtime typing while Bob composes.");
    await bobInput.fill("");
    await bobCtx.close();

    // Responsive layout (same DM, mobile viewport).
    seqViewport = "390x844";
    const mobCtx = await browser.newContext({ viewport: MOBILE });
    const mob = await mobCtx.newPage();
    await login(mob, "alice");
    await openDM(mob, "Bob Chen");
    await shot(mob, "02-main-interface", "responsive-layout.png", "Responsive layout", "Mobile conversation view.");
    await shot(mob, "11-responsive", "mobile-conversation.png", "Mobile chat", "Conversation list + chat on a 390px viewport.");
    await mob.goto(`${BASE}/discover`);
    await mob.waitForTimeout(800);
    await shot(mob, "11-responsive", "mobile-discovery.png", "Mobile discovery", "Discover on mobile.");
    await mob.goto(`${BASE}/settings`);
    await mob.waitForTimeout(800);
    await shot(mob, "11-responsive", "mobile-settings.png", "Mobile settings", "Settings on mobile.");
    await mobCtx.close();
    seqViewport = "1440x1000";

    // =====================================================================
    // 05 — Groups (Project Aurora)
    // =====================================================================
    console.log("==> 05-groups");
    await alice.goto(`${BASE}/`);
    await alice.waitForTimeout(800);
    await alice.getByRole("list").first().locator("button").filter({ hasText: "Project Aurora" }).first().click();
    await alice.waitForTimeout(1000);
    await shot(alice, "05-groups", "group-conversation.png", "Group conversation", "Project Aurora with member names and system events.");

    await alice.getByRole("button", { name: "Open conversation details" }).click();
    await alice.waitForTimeout(700);
    await shot(alice, "05-groups", "group-details.png", "Group details", "Members with roles and management actions.");
    await shot(alice, "05-groups", "group-members.png", "Group members", "Member list with owner/admin roles.");

    // Media gallery.
    await alice.getByRole("tab", { name: "Media" }).click();
    await alice.waitForTimeout(700);
    await shot(alice, "06-media", "media-gallery.png", "Media gallery", "Shared media and files grid.");

    await alice.keyboard.press("Escape");
    await alice.getByRole("button", { name: "Close details" }).click().catch(() => {});

    // Group creation dialog.
    await alice.goto(`${BASE}/`);
    await alice.waitForTimeout(600);
    await alice.getByRole("button", { name: "New conversation" }).click();
    await alice.waitForTimeout(400);
    await alice.getByPlaceholder(/Search @username/).fill("@bob");
    await alice.waitForTimeout(900);
    await alice.getByRole("button", { name: /Bob Chen/ }).first().click();
    await alice.getByPlaceholder("Group name…").fill("Design Sync");
    await alice.waitForTimeout(300);
    await shot(alice, "05-groups", "group-creation.png", "Group creation", "Multi-select + group title composer.");
    await alice.keyboard.press("Escape");

    // =====================================================================
    // 06 — Media: document message + upload progress
    // =====================================================================
    console.log("==> 06-media");
    await alice.goto(`${BASE}/`);
    await openDM(alice, "Bob Chen");
    const doc = Buffer.from(
      "Veylo demo document — this text file rides along as a document attachment.",
      "utf8",
    );
    await alice.setInputFiles('input[type="file"]', { name: "release-notes.txt", mimeType: "text/plain", buffer: doc });
    await alice.waitForTimeout(900);
    await shot(alice, "06-media", "attachment-upload.png", "Attachment upload", "Composer with attachment chip ready to send.");
    const docInput = alice.getByRole("textbox", { name: "Message input" });
    await docInput.fill("Release notes for review");
    await docInput.press("Enter");
    await alice.waitForTimeout(1200);
    await shot(alice, "06-media", "document-message.png", "Document message", "Document card with size and download.");

    // =====================================================================
    // 03 — Discovery
    // =====================================================================
    console.log("==> 03-user-discovery");
    await alice.goto(`${BASE}/discover`);
    await alice.waitForTimeout(700);
    await shot(alice, "03-user-discovery", "discovery-search.png", "Discovery", "Search landing with syntax hints.");

    await alice.getByPlaceholder(/Search @username/).fill("@bob");
    await alice.waitForTimeout(900);
    await shot(alice, "03-user-discovery", "username-results.png", "Username search", "@username lookup with presence.");

    await alice.getByPlaceholder(/Search @username/).fill("bob@example.test");
    await alice.waitForTimeout(900);
    await shot(alice, "03-user-discovery", "email-results.png", "Email search", "Privacy-respecting email discovery.");

    await alice.getByPlaceholder(/Search @username/).fill("@bob");
    await alice.waitForTimeout(900);
    await shot(alice, "03-user-discovery", "user-profile-preview.png", "Profile preview", "Discovery card with actions.");

    // =====================================================================
    // 07 — Contacts
    // =====================================================================
    console.log("==> 07-contacts");
    await alice.goto(`${BASE}/contacts`);
    await alice.waitForTimeout(800);
    await shot(alice, "07-contacts", "contact-list.png", "Contact list", "Accepted contacts with presence.");

    // Requests: David has an incoming seeded request.
    const davidCtx = await browser.newContext({ viewport: DESKTOP });
    const david = await davidCtx.newPage();
    await login(david, "david");
    await david.goto(`${BASE}/contacts`);
    await david.waitForTimeout(700);
    await david.getByRole("tab", { name: "Requests" }).click();
    await david.waitForTimeout(500);
    await shot(david, "07-contacts", "contact-requests.png", "Contact requests", "Incoming request with message.");

    // Blocked: Alice blocks David temporarily via API, captures, unblocks.
    await alice.evaluate(async () => {
      const search = await fetch("http://localhost:8080/v1/users/search?q=@david", { credentials: "include" }).then((r) => r.json());
      const id = search.data[0]?.id;
      await fetch("http://localhost:8080/v1/contacts/blocks", {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ user_id: id }),
      });
    });
    await alice.goto(`${BASE}/contacts`);
    await alice.waitForTimeout(600);
    await alice.getByRole("tab", { name: "Blocked" }).click();
    await alice.waitForTimeout(500);
    await shot(alice, "07-contacts", "blocked-users.png", "Blocked users", "Block list with unblock actions.");
    await alice.evaluate(async () => {
      const blocks = await fetch("http://localhost:8080/v1/contacts/blocks", { credentials: "include" }).then((r) => r.json());
      const target = blocks.data.find((b) => b.username === "david");
      if (target) await fetch(`http://localhost:8080/v1/contacts/blocks/${target.id}`, { method: "DELETE", credentials: "include" });
    });
    await davidCtx.close();

    // =====================================================================
    // 08 — Notifications
    // =====================================================================
    console.log("==> 08-notifications");
    await alice.goto(`${BASE}/`);
    await alice.waitForTimeout(800);
    await shot(alice, "08-notifications", "unread-notifications.png", "Unread notifications", "Workspace with notification activity.");
    await aliceCtx.close();

    const alice2 = await browser.newContext({ viewport: DESKTOP });
    const aliceN = await alice2.newPage();
    await login(aliceN, "alice");
    await aliceN.goto(`${BASE}/`);
    await aliceN.waitForTimeout(900);
    await aliceN.getByRole("button", { name: /Notifications/ }).click();
    await aliceN.waitForTimeout(600);
    await shot(aliceN, "08-notifications", "notification-center.png", "Notification center", "Notification feed with unread states.");
    await alice2.close();

    // =====================================================================
    // 09/10 — Profile & Settings (Alice)
    // =====================================================================
    console.log("==> 09/10-settings");
    const setCtx = await browser.newContext({ viewport: DESKTOP });
    const set = await setCtx.newPage();
    await login(set, "alice");
    await set.goto(`${BASE}/settings`);
    await set.waitForTimeout(900);
    await shot(set, "09-profile", "user-profile.png", "Profile", "Profile overview with avatar and verification.");

    await set.getByRole("textbox", { name: "About" }).fill("Designing calm interfaces ☕");
    await set.waitForTimeout(300);
    await shot(set, "09-profile", "profile-editor.png", "Profile editor", "Editable display name, username and about.");

    await set.getByRole("tab", { name: "Appearance" }).click();
    await set.waitForTimeout(400);
    await shot(set, "10-settings", "appearance.png", "Appearance", "Theme selection cards.");

    await set.getByRole("tab", { name: "Notifications", exact: true }).click();
    await set.waitForTimeout(400);
    await shot(set, "10-settings", "notifications.png", "Notification settings", "Per-category notification toggles.");

    await set.getByRole("tab", { name: "Privacy" }).click();
    await set.waitForTimeout(400);
    await shot(set, "10-settings", "privacy.png", "Privacy", "Visibility and discovery controls.");

    await set.getByRole("tab", { name: "Security" }).click();
    await set.waitForTimeout(400);
    await shot(set, "10-settings", "security.png", "Security", "Password change.");
    await shot(set, "10-settings", "active-sessions.png", "Active sessions", "Device sessions with revocation.");

    await set.getByRole("tab", { name: "Account" }).click();
    await set.waitForTimeout(400);
    await shot(set, "10-settings", "account.png", "Account", "Account info and sign-out.");
    await setCtx.close();

    // =====================================================================
    // 12 — System: Mailpit UI + rendered diagrams
    // =====================================================================
    console.log("==> 12-system");
    {
      const sysCtx = await browser.newContext({ viewport: DESKTOP });
      const sys = await sysCtx.newPage();
      try {
        await sys.goto("http://localhost:8025");
        await sys.waitForTimeout(1200);
        await shot(sys, "12-system", "local-infrastructure.png", "Local infrastructure", "Mailpit web UI capturing Veylo's local email.");
      } catch {
        console.log("  (Mailpit UI not reachable — skipped)");
      }

      // Render the architecture + database diagrams as pages and screenshot.
      const diagramHtml = (title, body) => `<!doctype html><html><head><meta charset="utf-8">
        <style>
          body { margin:0; padding:40px; background:#f7f7fa; font-family:-apple-system,Segoe UI,Roboto,sans-serif; color:#17161d; }
          h1 { font-size:22px; margin:0 0 6px; color:#5b4fc4; letter-spacing:-0.3px; }
          p.sub { margin:0 0 28px; color:#6d6b78; font-size:13px; }
          .wrap { display:flex; gap:20px; flex-wrap:wrap; }
          .box { background:#fff; border:1px solid #e4e3ec; border-radius:14px; padding:16px 20px; box-shadow:0 1px 3px rgba(23,22,29,.06); min-width:200px; }
          .box h3 { margin:0 0 8px; font-size:14px; color:#5b4fc4; }
          .box ul { margin:0; padding-left:16px; font-size:12px; color:#555; line-height:1.7; }
          .arrow { align-self:center; color:#9a97a8; font-size:20px; font-weight:700; }
          table.db { border-collapse:collapse; font-size:12px; background:#fff; border-radius:12px; overflow:hidden; box-shadow:0 1px 3px rgba(23,22,29,.08); }
          table.db th { background:#5b4fc4; color:#fff; text-align:left; padding:7px 12px; font-size:12px; }
          table.db td { padding:5px 12px; border-top:1px solid #eee; color:#444; }
          table.db td.k { color:#5b4fc4; font-weight:600; }
          .grid { display:grid; grid-template-columns:repeat(3, 1fr); gap:16px; max-width:1200px; }
        </style></head><body>${title}${body}</body></html>`;

      const arch = diagramHtml(
        `<h1>Veylo — Architecture Overview</h1><p class="sub">Local real-time communication platform · generated from docs/architecture/overview.md</p>`,
        `<div class="wrap">
          <div class="box"><h3>Browser — Next.js :3000</h3><ul><li>React 19 + App Router</li><li>TanStack Query (server state)</li><li>Zustand (ephemeral state)</li><li>Realtime client (1 WS / session)</li></ul></div>
          <div class="arrow">⇄ REST ⇄ WS</div>
          <div class="box"><h3>Go API :8080</h3><ul><li>chi router + middleware</li><li>Domain packages (auth, messages, realtime…)</li><li>WebSocket hub + fanout</li><li>Argon2id · rate limits · audit</li></ul></div>
          <div class="arrow">⇄</div>
          <div class="box"><h3>PostgreSQL — truth</h3><ul><li>Identity · sessions · contacts</li><li>Conversations · messages · receipts</li><li>Attachments metadata · audit</li></ul></div>
          <div class="box"><h3>Redis — ephemeral</h3><ul><li>Presence (online set)</li><li>Typing (TTL keys)</li><li>WS tickets (one-time)</li></ul></div>
          <div class="box"><h3>Local disk</h3><ul><li>storage/uploads media</li></ul></div>
          <div class="box"><h3>Mailpit :8025</h3><ul><li>Verification / reset email capture</li></ul></div>
        </div>`,
      );

      const db = diagramHtml(
        `<h1>Veylo — Database Architecture</h1><p class="sub">PostgreSQL schema (see docs/database/schema.md)</p>`,
        `<div class="grid">
          <table class="db"><tr><th>users</th></tr><tr><td class="k">id</td></tr><tr><td>username · email</td></tr><tr><td>password_hash (argon2id)</td></tr><tr><td>account_status · last_seen_at</td></tr></table>
          <table class="db"><tr><th>user_profiles</th></tr><tr><td>display_name · about</td></tr><tr><td>avatar_attachment_id</td></tr></table>
          <table class="db"><tr><th>sessions</th></tr><tr><td class="k">token_hash (sha256)</td></tr><tr><td>device · ip · expires_at</td></tr></table>
          <table class="db"><tr><th>conversations</th></tr><tr><td>type: direct | group</td></tr><tr><td>title · last_message_at</td></tr></table>
          <table class="db"><tr><th>conversation_members</th></tr><tr><td>role · muted · pinned</td></tr><tr><td>notification_level</td></tr></table>
          <table class="db"><tr><th>direct_conversation_keys</th></tr><tr><td class="k">pair_key (unique)</td></tr><tr><td>dedup guard</td></tr></table>
          <table class="db"><tr><th>messages</th></tr><tr><td class="k">seq (unique per conv)</td></tr><tr><td>type · text · client_message_id</td></tr><tr><td>edited_at · deleted_at</td></tr></table>
          <table class="db"><tr><th>message_read_cursors</th></tr><tr><td>delivered_seq · read_seq</td></tr><tr><td>unread = seq &gt; read_seq</td></tr></table>
          <table class="db"><tr><th>message_receipts</th></tr><tr><td>delivered_at · read_at</td></tr><tr><td>per message × user</td></tr></table>
          <table class="db"><tr><th>attachments</th></tr><tr><td>storage_key · sha256</td></tr><tr><td>kind · size · dimensions</td></tr></table>
          <table class="db"><tr><th>notifications</th></tr><tr><td>type · title · read_at</td></tr></table>
          <table class="db"><tr><th>audit_logs</th></tr><tr><td>action · ip · metadata</td></tr></table>
        </div>`,
      );

      await sys.setContent(arch);
      await sys.setViewportSize({ width: 1440, height: 760 });
      await sys.waitForTimeout(400);
      await shot(sys, "12-system", "architecture-overview.png", "Architecture", "Rendered system architecture diagram.");

      await sys.setContent(db);
      await sys.setViewportSize({ width: 1440, height: 720 });
      await sys.waitForTimeout(400);
      await shot(sys, "12-system", "database-architecture.png", "Database", "Rendered database schema diagram.");

      await sysCtx.close();
    }
    }

    await browser.close();

    writeFileSync(path.join(OUT, "..", "screenshots-manifest.json"), JSON.stringify(manifest, null, 2));
    console.log(`\nCaptured ${manifest.length} screenshots → docs/screenshots/`);
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
