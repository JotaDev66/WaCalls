import * as React from "react";
import { cn } from "@/lib/utils";
import type { StatusTone } from "@/lib/status";

const toneStyles: Record<StatusTone, { badge: string; dot: string }> = {
  ok: { badge: "bg-primary/15 text-primary", dot: "bg-primary" },
  neutral: {
    badge: "bg-muted text-muted-foreground",
    dot: "bg-muted-foreground",
  },
  warn: {
    badge: "bg-amber-500/15 text-amber-600 dark:text-amber-400",
    dot: "bg-amber-500",
  },
  danger: {
    badge: "bg-destructive/15 text-destructive",
    dot: "bg-destructive",
  },
};

type Props = React.HTMLAttributes<HTMLSpanElement> & {
  tone: StatusTone;
  pulse?: boolean;
};

export const StatusBadge = ({
  tone,
  pulse,
  className,
  children,
  ...props
}: Props) => (
  <span
    className={cn(
      "inline-flex items-center gap-1.5 rounded-sm px-2 py-0.5 font-mono text-[10px] font-semibold uppercase tracking-wide",
      toneStyles[tone].badge,
      className,
    )}
    {...props}
  >
    <span
      className={cn(
        "h-1.5 w-1.5 rounded-full",
        toneStyles[tone].dot,
        pulse && "tom-pulse",
      )}
    />
    {children}
  </span>
);
