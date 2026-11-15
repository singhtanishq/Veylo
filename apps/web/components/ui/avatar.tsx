"use client";

import * as React from "react";
import * as AvatarPrimitive from "@radix-ui/react-avatar";
import * as TooltipPrimitive from "@radix-ui/react-tooltip";
import { cn, avatarGradient, initials } from "@/lib/utils";
import { attachmentUrl } from "@/lib/api";

export function Avatar({
  src,
  name,
  userId,
  size = 40,
  online,
  showPresence = false,
  className,
}: {
  src?: string | null;
  name: string;
  userId: string;
  size?: number;
  online?: boolean;
  showPresence?: boolean;
  className?: string;
}) {
  const resolved = src ? attachmentUrl(src) : null;
  return (
    <span className={cn("relative inline-flex shrink-0", className)} style={{ width: size, height: size }}>
      <AvatarPrimitive.Root
        className="h-full w-full overflow-hidden rounded-full"
        style={{ width: size, height: size }}
      >
        <AvatarPrimitive.Fallback
          className="flex h-full w-full items-center justify-center text-white font-semibold"
          style={{
            background: avatarGradient(userId),
            fontSize: Math.max(11, size * 0.36),
            fontFamily: "var(--font-sora)",
          }}
          delayMs={resolved ? 300 : 0}
        >
          {initials(name)}
        </AvatarPrimitive.Fallback>
        {resolved && (
          <AvatarPrimitive.Image
            src={resolved}
            alt={name}
            className="h-full w-full object-cover"
          />
        )}
      </AvatarPrimitive.Root>
      {showPresence && (
        <span
          className={cn(
            "absolute bottom-0 right-0 block rounded-full border-2",
            online ? "bg-success" : "bg-muted/50",
          )}
          style={{ width: Math.max(9, size * 0.26), height: Math.max(9, size * 0.26) }}
          aria-label={online ? "Online" : "Offline"}
        />
      )}
    </span>
  );
}

// Tooltip ---------------------------------------------------------------

export function TooltipProvider({ children }: { children: React.ReactNode }) {
  return <TooltipPrimitive.Provider delayDuration={300}>{children}</TooltipPrimitive.Provider>;
}

export function Tooltip({
  content,
  children,
  side = "top",
}: {
  content: string;
  children: React.ReactNode;
  side?: "top" | "bottom" | "left" | "right";
}) {
  return (
    <TooltipPrimitive.Root>
      <TooltipPrimitive.Trigger asChild>{children}</TooltipPrimitive.Trigger>
      <TooltipPrimitive.Portal>
        <TooltipPrimitive.Content
          side={side}
          sideOffset={6}
          className="z-50 rounded-lg bg-foreground px-2.5 py-1.5 text-xs font-medium text-background shadow-lg"
        >
          {content}
        </TooltipPrimitive.Content>
      </TooltipPrimitive.Portal>
    </TooltipPrimitive.Root>
  );
}

// Skeleton ---------------------------------------------------------------

export function Skeleton({ className }: { className?: string }) {
  return <div className={cn("vy-skeleton rounded-xl", className)} />;
}
