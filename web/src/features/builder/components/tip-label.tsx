"use client";

import { InfoTip } from "@/components/info-tip";

// TipLabel is the small uppercase label of a field, with an "i" that explains it.
export function TipLabel({ htmlFor, help, children }: { htmlFor: string; help: string; children: string }) {
  return (
    <span className="flex items-center gap-1.5">
      <label htmlFor={htmlFor} className="text-[11px] tracking-wider text-muted-foreground uppercase">
        {children}
      </label>
      <InfoTip label={children}>{help}</InfoTip>
    </span>
  );
}
