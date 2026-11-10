"use client";

import { usePathname } from "next/navigation";

import { buildTrail } from "@/features/shell/trail";
import type { ShellScope } from "@/features/shell/types";

import type { Theme } from "@/features/theme/theme";

import { Breadcrumb } from "./breadcrumb";
import { ThemeToggle } from "./theme-toggle";

// A client component for one reason: the trail follows the address, and a
// layout does not render again when the page under it changes.
export function AppHeader({ organization, scope, theme }: { organization: string; scope: ShellScope | null; theme: Theme }) {
  const trail = buildTrail(usePathname(), organization, scope);

  return (
    <header className="flex min-h-12 items-center border-b px-4 py-2 md:px-page-x">
      <Breadcrumb trail={trail} />
      <div className="ml-auto pl-3">
        <ThemeToggle initial={theme} />
      </div>
    </header>
  );
}
