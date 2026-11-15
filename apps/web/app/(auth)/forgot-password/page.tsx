"use client";

import * as React from "react";
import Link from "next/link";
import { toast } from "sonner";
import { KeyRound, MailCheck } from "lucide-react";
import { api } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Input, Label } from "@/components/ui/input";

export default function ForgotPasswordPage() {
  const [email, setEmail] = React.useState("");
  const [sent, setSent] = React.useState(false);
  const [busy, setBusy] = React.useState(false);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    try {
      await api.forgotPassword(email);
      setSent(true);
    } catch {
      // Always show the generic "sent" state (anti-enumeration).
      setSent(true);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="vy-animate-in">
      {sent ? (
        <div>
          <div className="flex h-12 w-12 items-center justify-center rounded-2xl bg-primary-soft text-primary">
            <MailCheck size={24} />
          </div>
          <h2 className="mt-4 text-2xl font-semibold tracking-tight" style={{ fontFamily: "var(--font-sora)" }}>
            Check Mailpit
          </h2>
          <p className="mt-2 text-sm text-muted leading-relaxed">
            If an account exists for <span className="font-medium text-foreground">{email}</span>, a
            password-reset link is waiting in Mailpit. The link expires in one hour and can be used once.
          </p>
          <a href="http://localhost:8025" target="_blank" rel="noreferrer" className="mt-6 block">
            <Button size="lg" className="w-full">Open Mailpit ↗</Button>
          </a>
          <Link href="/login" className="mt-4 block text-center text-sm text-muted hover:text-foreground">
            Back to sign in
          </Link>
        </div>
      ) : (
        <div>
          <div className="flex h-12 w-12 items-center justify-center rounded-2xl bg-primary-soft text-primary">
            <KeyRound size={24} />
          </div>
          <h2 className="mt-4 text-2xl font-semibold tracking-tight" style={{ fontFamily: "var(--font-sora)" }}>
            Reset your password
          </h2>
          <p className="mt-2 text-sm text-muted">
            Enter your account email and we&apos;ll send a reset link through the local mail server.
          </p>
          <form onSubmit={submit} className="mt-8 space-y-4">
            <div>
              <Label htmlFor="email">Email</Label>
              <Input
                id="email"
                type="email"
                className="mt-1.5"
                placeholder="you@example.test"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                required
              />
            </div>
            <Button type="submit" size="lg" className="w-full" loading={busy}>
              Send reset link
            </Button>
          </form>
          <Link href="/login" className="mt-5 block text-sm text-muted hover:text-foreground">
            Back to sign in
          </Link>
        </div>
      )}
    </div>
  );
}
