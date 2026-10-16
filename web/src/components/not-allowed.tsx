import { Lock } from "lucide-react";

export function NotAllowed({ children }: { children: React.ReactNode }) {
  return (
    <p className="flex items-center gap-2 text-sm text-muted-foreground">
      <Lock aria-hidden className="size-4 shrink-0" />
      {children}
    </p>
  );
}
