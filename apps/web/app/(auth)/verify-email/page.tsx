"use client";

import * as React from "react";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { toast } from "sonner";
import { MailCheck, CheckCircle2, AlertCircle } from "lucide-react";
import { api, ApiError } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Input, Label, FieldError } from "@/components/ui/input";

type Status = "input" | "verifying" | "success" | "error";

function VerifyEmailForm() {
  const params = useSearchParams();
  const router = useRouter();
  const [token, setToken] = React.useState(params.get("token") ?? "");
  const [status, setStatus] = React.useState<Status>("input");
  const [error, setError] = React.useState<string | null>(null);
  const [email, setEmail] = React.useState(params.get("email") ?? "");
  const [resent, setResent] = React.useState(false);

  const fromRegistration = params.get("registered") === "1" || params.get("pending") === "1";

  // Auto-verify when the link carries a token.
  React.useEffect(() => {
    const t = params.get("token");
    if (t) verify(t);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  async function verify(t: string) {
    setStatus("verifying");
    setError(null);
    try {
      await api.verifyEmail(t);
      setStatus("success");
      toast.success("Email verified — you can sign in now.");
    } catch (err) {
      setStatus("error");
      setError(err instanceof ApiError ? err.message : "Verification failed.");
    }
  }

  async function resend() {
    try {
      await api.resendVerification(email || undefined);
      setResent(true);
      toast.success("A fresh verification link is waiting in Mailpit.");
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "Could not resend the email.");
    }
  }

  return (
    <div className="vy-animate-in">
      {status === "success" ? (
        <div className="text-center">
          <CheckCircle2 className="mx-auto text-success" size={52} />
          <h2 className="mt-4 text-2xl font-semibold" style={{ fontFamily: "var(--font-sora)" }}>
            Email verified
          </h2>
          <p className="mt-2 text-sm text-muted">Your account is active. Time to say hello.</p>
          <Link href="/login" className="mt-6 inline-block w-full">
            <Button size="lg" className="w-full">Sign in</Button>
          </Link>
        </div>
      ) : status === "error" ? (
        <div className="text-center">
          <AlertCircle className="mx-auto text-danger" size={52} />
          <h2 className="mt-4 text-2xl font-semibold" style={{ fontFamily: "var(--font-sora)" }}>
            Verification problem
          </h2>
          <p className="mt-2 text-sm text-muted">{error}</p>
          <Button variant="outline" className="mt-6 w-full" onClick={() => setStatus("input")}>
            Enter a code manually
          </Button>
          <Button variant="ghost" className="mt-2 w-full" onClick={resend}>
            Send a new link
          </Button>
        </div>
      ) : (
        <div>
          <div className="flex h-12 w-12 items-center justify-center rounded-2xl bg-primary-soft text-primary">
            <MailCheck size={24} />
          </div>
          <h2 className="mt-4 text-2xl font-semibold tracking-tight" style={{ fontFamily: "var(--font-sora)" }}>
            {fromRegistration ? "Check your email" : "Verify your email"}
          </h2>
          <p className="mt-2 text-sm text-muted leading-relaxed">
            {fromRegistration
              ? "We sent a verification link to your address. Open Mailpit at localhost:8025 and click the button — or paste the token here."
              : "Paste the token from your verification email below."}
          </p>

          <form
            onSubmit={(e) => {
              e.preventDefault();
              verify(token.trim());
            }}
            className="mt-6 space-y-4"
          >
            <div>
              <Label htmlFor="token">Verification token</Label>
              <Input
                id="token"
                className="mt-1.5 font-mono text-xs"
                placeholder="Paste the token from the email link"
                value={token}
                onChange={(e) => setToken(e.target.value)}
                required
              />
            </div>
            {fromRegistration && (
              <div>
                <Label htmlFor="vemail">Email (for resend)</Label>
                <Input id="vemail" type="email" className="mt-1.5" placeholder="you@example.test" value={email} onChange={(e) => setEmail(e.target.value)} />
              </div>
            )}
            <FieldError message={error} />
            <Button type="submit" size="lg" className="w-full" loading={status === "verifying"}>
              Verify email
            </Button>
          </form>

          <div className="mt-5 flex items-center justify-between text-sm">
            <a href="http://localhost:8025" target="_blank" rel="noreferrer" className="text-primary hover:underline">
              Open Mailpit ↗
            </a>
            {resent ? (
              <span className="text-success">Sent! Check Mailpit.</span>
            ) : (
              <button onClick={resend} className="text-muted hover:text-foreground">
                Resend email
              </button>
            )}
          </div>

          <p className="mt-8 text-sm text-muted">
            Already verified?{" "}
            <Link href="/login" className="font-medium text-primary hover:underline">
              Sign in
            </Link>
          </p>
        </div>
      )}
    </div>
  );
}

export default function Page() {
  return (
    <React.Suspense>
      <VerifyEmailForm />
    </React.Suspense>
  );
}
