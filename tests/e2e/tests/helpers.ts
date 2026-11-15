// Shared E2E helpers: registration through the real Mailpit-captured email,
// login, and small UI conveniences.

import { expect, type Page, type BrowserContext } from "@playwright/test";

const MAILPIT = process.env.E2E_MAILPIT_URL ?? "http://localhost:8025";

export const DEMO_PASSWORD = "VeyloDemo!2026";

export interface E2EUser {
  username: string;
  email: string;
  password: string;
  displayName: string;
  page: Page;
}

let seq = 0;
export function uniqueUsername(prefix: string): string {
  seq += 1;
  return `${prefix}${Date.now().toString(36)}${seq}`;
}

// Pull the newest verification token for an email out of Mailpit's REST API.
export async function latestTokenFromMailpit(email: string): Promise<string> {
  const list = (await (
    await fetch(`${MAILPIT}/api/v1/search?query=${encodeURIComponent("to:" + email)}`)
  ).json()) as {
    messages: { ID: string; Subject: string }[];
  };
  const verify = list.messages.find((m) => m.Subject.toLowerCase().includes("verify"));
  if (!verify) throw new Error(`No verification email for ${email} in Mailpit`);
  const detail = (await (await fetch(`${MAILPIT}/api/v1/message/${verify.ID}`)).json()) as {
    Text: string;
  };
  const match = detail.Text.match(/token=([A-Za-z0-9_-]+)/);
  if (!match) throw new Error(`No token in email: ${detail.Text.slice(0, 200)}`);
  return match[1];
}

export async function latestResetTokenFromMailpit(email: string): Promise<string> {
  const list = (await (
    await fetch(`${MAILPIT}/api/v1/search?query=${encodeURIComponent("to:" + email)}`)
  ).json()) as {
    messages: { ID: string; Subject: string }[];
  };
  const reset = list.messages.find((m) => m.Subject.toLowerCase().includes("reset"));
  if (!reset) throw new Error(`No reset email for ${email} in Mailpit`);
  const detail = (await (await fetch(`${MAILPIT}/api/v1/message/${reset.ID}`)).json()) as {
    Text: string;
  };
  const match = detail.Text.match(/token=([A-Za-z0-9_-]+)/);
  if (!match) throw new Error(`No token in email: ${detail.Text.slice(0, 200)}`);
  return match[1];
}

// Full registration journey through the real UI + Mailpit verification.
export async function registerAndLogin(
  context: BrowserContext,
  username: string,
  displayName: string,
): Promise<E2EUser> {
  const page = await context.newPage();
  const email = `${username}@e2e.test`;
  const password = "E2ePassw0rd!x";

  await page.goto("/register");
  await page.getByRole("textbox", { name: "Display name" }).fill(displayName);
  await page.getByRole("textbox", { name: "Username" }).fill(username);
  await page.getByRole("textbox", { name: "Email" }).fill(email);
  await page.getByRole("textbox", { name: "Password" }).fill(password);
  await page.getByRole("button", { name: "Create account" }).click();

  // Redirected to verify-email with the pending banner.
  await expect(page.getByRole("heading", { name: "Check your email" })).toBeVisible({ timeout: 15_000 });

  // Verify through the real email token.
  const token = await latestTokenFromMailpit(email);
  await page.goto(`/verify-email?token=${token}`);
  await expect(page.getByRole("heading", { name: "Email verified" })).toBeVisible({ timeout: 15_000 });

  // Login.
  await page.goto("/login");
  await page.getByRole("textbox", { name: "Email or username" }).fill(username);
  await page.getByRole("textbox", { name: "Password", exact: true }).fill(password);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByRole("heading", { name: "Messages" })).toBeVisible({ timeout: 15_000 });

  return { username, email, password, displayName, page };
}

export async function openConversation(page: Page, otherName: string) {
  await page.getByRole("button", { name: new RegExp(otherName.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")) }).first().click();
  await expect(page.getByRole("button", { name: "Send message" })).toBeVisible({ timeout: 10_000 });
}

export async function sendMessage(page: Page, text: string) {
  const input = page.getByRole("textbox", { name: "Message input" });
  await input.fill(text);
  await input.press("Enter");
  // The persisted message appears (not just the optimistic one).
  await expect(page.getByText(text).last()).toBeVisible({ timeout: 10_000 });
}
