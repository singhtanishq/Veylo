"use client";

// Lightweight categorized emoji picker (no heavy dependency).

import * as React from "react";
import { cn } from "@/lib/utils";

const CATEGORIES: { label: string; emojis: string[] }[] = [
  {
    label: "Smileys",
    emojis: ["😀","😄","😁","🥰","😍","🤩","😌","🙂","😉","😊","🙃","😅","😂","🤣","🥲","😍","😘","😗","🤔","🤨","😐","😴","🤤","😪","😵","🤯","🥳","😎","🤓","🧐","😕","😟","🙁","😮","😲","🥺","😢","😭","😤","😠","😡","🤬","😈","🤡","💀","👋","🤚","✋","🤙","👍","👎","✌️","🤞","🤝","🙏","💪","👀","🧠"],
  },
  {
    label: "Gestures & people",
    emojis: ["💃","🕺","🧘","🏃","🚶","🧗","🏊","🚴","👨‍💻","👩‍💻","👨‍🎨","👩‍🎨","🧑‍🔧","🎉","🥂","🍻","🎁","🏆","🥇","🎯"],
  },
  {
    label: "Objects",
    emojis: ["❤️","🧡","💛","💚","💙","💜","🖤","🔥","✨","⭐","🌟","💫","💥","💯","✅","❌","⚠️","🔒","🔑","📎","📌","📎","💬","💭","🔔","🎵","🎧","📱","💻","⌨️","🖥️","📷","🔋","☕","🍵","🍺","🍕","🍔","🍩","🍪","🎂","🍫","🍎","🥑","🌸","🌈","☀️","🌙","⚡","❄️"],
  },
  {
    label: "Travel & symbols",
    emojis: ["🚗","✈️","🚀","🛸","⛵","🏠","🏢","🗺️","🧭","🏝️","🏔️","🌊","🕐","📅","📈","📉","📊","🧩","🎮","🎨","🎬","🎤","📞","✉️","📨","🖊️","📚","📖","🔗","🆗","🆒","🔝"],
  },
];

export function EmojiPicker({ onPick, className }: { onPick: (emoji: string) => void; className?: string }) {
  const [tab, setTab] = React.useState(0);
  return (
    <div className={cn("w-[280px]", className)}>
      <div className="flex gap-1 border-b border-border pb-2">
        {CATEGORIES.map((c, i) => (
          <button
            key={c.label}
            onClick={() => setTab(i)}
            className={cn(
              "rounded-lg px-2 py-1 text-[11px] font-medium transition-colors",
              tab === i ? "bg-primary-soft text-primary" : "text-muted hover:bg-surface-2",
            )}
          >
            {c.label.split(" ")[0]}
          </button>
        ))}
      </div>
      <div className="mt-2 grid max-h-[180px] grid-cols-8 gap-0.5 overflow-y-auto">
        {CATEGORIES[tab].emojis.map((e, i) => (
          <button
            key={`${e}-${i}`}
            onClick={() => onPick(e)}
            className="flex h-8 w-8 items-center justify-center rounded-lg text-[17px] hover:bg-surface-2"
            aria-label={`Insert ${e}`}
          >
            {e}
          </button>
        ))}
      </div>
    </div>
  );
}
