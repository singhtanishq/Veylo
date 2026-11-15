import { redirect } from "next/navigation";
import { cookies } from "next/headers";
import { api } from "@/lib/api";
import { AppShell } from "@/components/layout/app-shell";
import { Providers } from "@/app/providers";

export default async function AppLayout({ children }: { children: React.ReactNode }) {
  const cookieStore = await cookies();
  const cookieHeader = cookieStore
    .getAll()
    .map((c) => `${c.name}=${c.value}`)
    .join("; ");

  let me = null;
  try {
    me = await api.meServer(cookieHeader);
  } catch {
    me = null;
  }
  if (!me) {
    redirect("/login");
  }

  return (
    <Providers>
      <AppShell>{children}</AppShell>
    </Providers>
  );
}
