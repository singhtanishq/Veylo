import Link from "next/link";

export default function AuthLayout({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex min-h-screen">
      {/* Brand panel */}
      <div
        className="hidden lg:flex lg:w-[44%] flex-col justify-between p-10 text-white relative overflow-hidden"
        style={{ background: "linear-gradient(160deg,#5b4fc4 0%,#4638a8 55%,#2f2873 100%)" }}
      >
        <div
          aria-hidden
          className="absolute -top-24 -right-24 h-96 w-96 rounded-full opacity-20"
          style={{ background: "radial-gradient(circle,#a89bff,transparent 70%)" }}
        />
        <div
          aria-hidden
          className="absolute bottom-[-120px] left-[-60px] h-[420px] w-[420px] rounded-full opacity-15"
          style={{ background: "radial-gradient(circle,#e0b34d,transparent 70%)" }}
        />
        <Link href="/" className="relative flex items-center gap-2.5">
          <VeyloMark size={34} />
          <span className="text-xl font-semibold tracking-tight" style={{ fontFamily: "var(--font-sora)" }}>
            Veylo
          </span>
        </Link>
        <div className="relative max-w-md">
          <h1 className="text-[34px] leading-[1.15] font-semibold tracking-tight" style={{ fontFamily: "var(--font-sora)" }}>
            Conversations without boundaries.
          </h1>
          <p className="mt-4 text-[15px] leading-relaxed text-white/75">
            A private, real-time communication platform. Persistent conversations, instant
            delivery, presence and groups — everything stays on your machine.
          </p>
          <ul className="mt-8 space-y-3 text-sm text-white/80">
            <li className="flex items-center gap-3"><Dot /> Real-time delivery with read receipts</li>
            <li className="flex items-center gap-3"><Dot /> Direct messages, groups and media</li>
            <li className="flex items-center gap-3"><Dot /> Multi-device session sync</li>
            <li className="flex items-center gap-3"><Dot /> Local-first — no cloud required</li>
          </ul>
        </div>
        <p className="relative text-xs text-white/50">Local development build · Mailpit captures all email</p>
      </div>

      {/* Form panel */}
      <div className="flex-1 flex items-center justify-center p-6">
        <div className="w-full max-w-[400px]">{children}</div>
      </div>
    </div>
  );
}

function Dot() {
  return <span className="h-1.5 w-1.5 rounded-full bg-white/60" />;
}

export function VeyloMark({ size = 28 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 40 40" fill="none" aria-hidden>
      <rect width="40" height="40" rx="11" fill="rgba(255,255,255,0.14)" />
      <path
        d="M11 13.5L20 28.5L29 13.5"
        stroke="white"
        strokeWidth="3.2"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
      <circle cx="20" cy="12" r="2.6" fill="#e0b34d" />
    </svg>
  );
}
