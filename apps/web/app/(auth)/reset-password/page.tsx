"use client";

import * as React from "react";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { toast } from "sonner";
import { ShieldCheck } from "lucide-react";
import { api, ApiError } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Input, Label, FieldError } from "@/components/ui/input";

function ResetPasswordForm() {
  const params = useSearchParams();
  const router = useRouter();
  const token = params.get("token") ?? "";
  const [password, setPassword] = React.useState("");
  const [confirm, setConfirm] = React.useState("");
  const [error, setError] = React.useState<string | null>(null);
  const [busy, setBusy] = React.useState(false);

  const valid = password.length >= 8 && /[A-Z]/.test(password) && /[a-z]/.test(password) && /\d/.test(password);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    if (password !== confirm) {
      setError("Passwords do not match.");
      return;
    }
    setError(null);
    setBusy(true);
    try {
      await api.resetPassword(token, password);
      toast.success("Password updated. All other sessions were signed out.");
      router.push("/login");
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Reset failed.");
    } finally {
      setBusy(false);
    }
  }

  if (!token) {
    return (
      <div className="text-center vy-animate-in">
        <h2 className="text-2xl font-semibold" style={{ fontFamily: "var(--font-sora)" }}>Missing token</h2>
        <p className="mt-2 text-sm text-muted">Open the reset link from your email — it includes the token.</p>
        <Link href="/forgot-password" className="mt-6 inline-block text-sm font-medium text-primary hover:underline">
          Request a new link
        </Link>
      </div>
    );
  }

  return (
    <div className="vy-animate-in">
      <div className="flex h-12 w-12 items-center justify-center rounded-2xl bg-primary-soft text-primary">
        <ShieldCheck size={24} />
      </div>
      <h2 className="mt-4 text-2xl font-semibold tracking-tight" style={{ fontFamily: "var(--font-sora)" }}>
        Choose a new password
      </h2>
      <p className="mt-2 text-sm text-muted">For your security, all active sessions will be signed out.</p>

      <form onSubmit={submit} className="mt-8 space-y-4">
        <div>
          <Label htmlFor="password">New password</Label>
          <Input
            id="password"
            type="password"
            className="mt-1.5"
            autoComplete="new-password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            required
          />
          {!valid && password.length > 0 && (
            <p className="mt-1.5 text-xs text-muted">8+ characters with upper, lower and a number.</p>
          )}
        </div>
        <div>
          <Label htmlFor="confirm">Confirm password</Label>
          <Input
            id="confirm"
            type="password"
            className="mt-1.5"
            autoComplete="new-password"
            value={confirm}
            onChange={(e) => setConfirm(e.target.value)}
            required
          />
        </div>
        <FieldError message={error} />
        <Button type="submit" size="lg" className="w-full" loading={busy} disabled={!valid}>
          Update password
        </Button>
      </form>
    </div>
  );
}

export default function Page() {
  return (
    <React.Suspense>
      <ResetPasswordForm />
    </React.Suspense>
  );
}
