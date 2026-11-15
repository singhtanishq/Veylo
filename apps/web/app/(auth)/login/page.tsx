"use client";

import * as React from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { toast } from "sonner";
import { Eye, EyeOff } from "lucide-react";
import { api, ApiError } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Input, Label, FieldError } from "@/components/ui/input";

export default function LoginPage() {
  const router = useRouter();
  const [identifier, setIdentifier] = React.useState("");
  const [password, setPassword] = React.useState("");
  const [showPassword, setShowPassword] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);
  const [busy, setBusy] = React.useState(false);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setError(null);
    setBusy(true);
    try {
      await api.login({ identifier, password });
      toast.success("Welcome back!");
      router.push("/");
      router.refresh();
    } catch (err) {
      if (err instanceof ApiError && err.code === "email_not_verified") {
        router.push(`/verify-email?pending=1&identifier=${encodeURIComponent(identifier)}`);
        return;
      }
      setError(err instanceof Error ? err.message : "Login failed");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="vy-animate-in">
      <h2 className="text-2xl font-semibold tracking-tight" style={{ fontFamily: "var(--font-sora)" }}>
        Welcome back
      </h2>
      <p className="mt-1.5 text-sm text-muted">Sign in to continue your conversations.</p>

      <form onSubmit={submit} className="mt-8 space-y-4">
        <div>
          <Label htmlFor="identifier">Email or username</Label>
          <Input
            id="identifier"
            className="mt-1.5"
            placeholder="alice@example.test"
            autoComplete="username"
            value={identifier}
            onChange={(e) => setIdentifier(e.target.value)}
            required
          />
        </div>
        <div>
          <Label htmlFor="password">Password</Label>
          <div className="relative mt-1.5">
            <Input
              id="password"
              type={showPassword ? "text" : "password"}
              placeholder="Your password"
              autoComplete="current-password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              required
              className="pr-11"
            />
            <button
              type="button"
              onClick={() => setShowPassword((v) => !v)}
              className="absolute right-3 top-1/2 -translate-y-1/2 text-muted hover:text-foreground"
              aria-label={showPassword ? "Hide password" : "Show password"}
            >
              {showPassword ? <EyeOff size={17} /> : <Eye size={17} />}
            </button>
          </div>
        </div>

        <FieldError message={error} />

        <Button type="submit" size="lg" className="w-full" loading={busy}>
          Sign in
        </Button>
      </form>

      <div className="mt-5 flex items-center justify-between text-sm">
        <Link href="/forgot-password" className="text-muted hover:text-foreground transition-colors">
          Forgot password?
        </Link>
        <Link href="/register" className="font-medium text-primary hover:underline">
          Create an account
        </Link>
      </div>

      <div className="mt-10 rounded-xl bg-surface-2 p-4 text-xs text-muted leading-relaxed">
        <span className="font-semibold text-foreground">Demo accounts</span> (after{" "}
        <code className="text-foreground">make seed</code>): <code>alice</code>, <code>bob</code>,{" "}
        <code>charlie</code>, <code>david</code> — password <code>VeyloDemo!2026</code>.
      </div>
    </div>
  );
}
