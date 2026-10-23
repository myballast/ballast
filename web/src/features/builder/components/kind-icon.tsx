import {
  ArrowUpRight,
  Crosshair,
  Equal,
  GitBranch,
  Repeat,
  ShieldCheck,
  Square,
  Timer,
  type LucideIcon,
} from "lucide-react";

import type { KindInfo } from "../kinds";

const icons: Record<KindInfo["icon"], LucideIcon> = {
  request: ArrowUpRight,
  extract: Crosshair,
  set: Equal,
  condition: GitBranch,
  loop: Repeat,
  wait: Timer,
  assert: ShieldCheck,
  end: Square,
};

// The icon of a kind of node. Never the only thing that tells kinds apart: the
// name is always written beside it.
export function KindIcon({ icon, className }: { icon: KindInfo["icon"]; className?: string }) {
  const Icon = icons[icon];
  return <Icon aria-hidden className={className ?? "size-3.5"} />;
}
