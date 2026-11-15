"use client";

import * as React from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { toast } from "sonner";
import { Check } from "lucide-react";
import { api, ApiError } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Input, Label, FieldError } from "@/components/ui/input";

const RULES = [
  { label: "8+ characters", test: (p: string) => p.length >= 8 },
  { label: "Upper & lowercase", test: (p: string) => /[A-Z]/.test(p) && /[a-z]/.test(p) },
  { label: "A number", test: (p: string) => /\d/.test(p) },
];

export default function RegisterPage() {
  const router = useRouter();
  const [form, setForm] = React.useState({ display_name: "", username: "", email: "", password: "" });
  const [errors, setErrors] = React.useState<Record<string, string>>({});
  const [busy, setBusy] = React.useState(false);

  const set = (k: keyof typeof form) => (e: React.ChangeEvent<HTMLInputElement>) => {
    let v = e.target.value;
    if (k === "username") v = v.toLowerCase().replace(/[^a-z0-9_]/g, "");
    setForm((f) => ({ ...f, [k]: v }));
    setErrors((er) => ({ ...er, [k]: "" }));
  };

  // Live username availability (debounced).
  const [available, setAvailable] = React.useState<boolean | null>(null);
  React.useEffect(() => {
    const u = form.username;
    if (u.length < 3) {
      setAvailable(null);
      return;
    }
    const t = setTimeout(async () => {
      try {
        const res = await api.usernameAvailability(u);
        setAvailable(res.available);
      } catch {
        setAvailable(null);
      }
    }, 350);
    return () => clearTimeout(t);
  }, [form.username]);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setErrors({});
    setBusy(true);
    try {
      await api.register(form);
      toast.success("Account created — check Mailpit for your verification link.");
      router.push(`/verify-email?registered=1&email=${encodeURIComponent(form.email)}`);
    } catch (err) {
      if (err instanceof ApiError) {
        if (err.code === "email_exists") setErrors({ email: "An account with this email already exists." });
        else if (err.code === "username_exists") setErrors({ username: "That username is already taken." });
        else if (err.details && Array.isArray(err.details)) {
          const fieldErrors: Record<string, string> = {};
          for (const d of err.details as { field: string; message: string }[]) {
            fieldErrors[d.field] = d.message;
          }
          setErrors(fieldErrors);
        } else setErrors({ form: err.message });
      } else {
        setErrors({ form: "Something went wrong. Please try again." });
      }
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="vy-animate-in">
      <h2 className="text-2xl font-semibold tracking-tight" style={{ fontFamily: "var(--font-sora)" }}>
        Create your account
      </h2>
      <p className="mt-1.5 text-sm text-muted">Join Veylo and start talking in minutes.</p>

      <form onSubmit={submit} className="mt-8 space-y-4">
        <div>
          <Label htmlFor="display_name">Display name</Label>
          <Input id="display_name" className="mt-1.5" placeholder="Alice Morgan" value={form.display_name} onChange={set("display_name")} required maxLength={80} />
          <FieldError message={errors.display_name} />
        </div>
        <div>
          <Label htmlFor="username">Username</Label>
          <div className="relative mt-1.5">
            <span className="absolute left-3.5 top-1/2 -translate-y-1/2 text-muted">@</span>
            <Input id="username" className="pl-9 pr-9" placeholder="alice" value={form.username} onChange={set("username")} required maxLength={24} />
            {available !== null && (
              <span
                className={`absolute right-3 top-1/2 -translate-y-1/2 ${available ? "text-success" : "text-danger"}`}
                title={available ? "Available" : "Already taken"}
              >
                {available ? <Check size={16} /> : "✕"}
              </span>
            )}
          </div>
          <FieldError message={errors.username ?? (available === false ? "That username is already taken." : null)} />
        </div>
        <div>
          <Label htmlFor="email">Email</Label>
          <Input id="email" type="email" className="mt-1.5" placeholder="alice@example.test" value={form.email} onChange={set("email")} required />
          <FieldError message={errors.email} />
        </div>
        <div>
          <Label htmlFor="password">Password</Label>
          <Input id="password" type="password" className="mt-1.5" placeholder="Choose a strong password" value={form.password} onChange={set("password")} autoComplete="new-password" required />
          <div className="mt-2 flex gap-3">
            {RULES.map((r) => {
              const ok = r.test(form.password);
              return (
                <span key={r.label} className={`text-[11px] flex items-center gap-1 ${ok ? "text-success" : "text-muted"}`}>
                  <span className={`h-1 w-1 rounded-full ${ok ? "bg-success" : "bg-muted"}`} /> {r.label}
                </span>
              );
            })}
          </div>
          <FieldError message={errors.password} />
        </div>

        <FieldError message={errors.form} />

        <Button type="submit" size="lg" className="w-full" loading={busy}>
          Create account
        </Button>
      </form>

      <p className="mt-5 text-sm text-muted">
        Already have an account?{" "}
        <Link href="/login" className="font-medium text-primary hover:underline">
          Sign in
        </Link>
      </p>
    </div>
  );
}
