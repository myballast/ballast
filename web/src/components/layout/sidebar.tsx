"use client";

import { PanelLeftClose, PanelLeftOpen } from "lucide-react";
import { usePathname } from "next/navigation";
import { createContext, useCallback, useContext, useEffect, useState } from "react";

import { cn } from "@/lib/utils";

import { collapsedFor, sidebarCookie, type SidebarPreference } from "./sidebar-state";

const Toggle = createContext<{ collapsed: boolean; toggle: () => void } | null>(null);

// SidebarFrame is the sidebar of the wide screens. It is a client component for
// one reason: it can be collapsed, and the choice is a cookie, so that the server
// draws the same state the next time and the page does not jump.
export function SidebarFrame({
  preference,
  children,
}: {
  preference: SidebarPreference | undefined;
  children: React.ReactNode;
}) {
  const pathname = usePathname();
  const [choice, setChoice] = useState(preference);
  const collapsed = collapsedFor(choice, pathname);

  const toggle = useCallback(() => {
    const next: SidebarPreference = collapsed ? "open" : "collapsed";
    setChoice(next);
    document.cookie = `${sidebarCookie}=${next}; path=/; max-age=31536000; samesite=lax`;
  }, [collapsed]);

  // Ctrl/⌘ + B, as in editors — except where it means something else, in a field.
  useEffect(() => {
    function onKey(event: KeyboardEvent) {
      if (!(event.metaKey || event.ctrlKey) || event.key.toLowerCase() !== "b") return;
      const target = event.target as HTMLElement | null;
      if (target && (target.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(target.tagName))) return;
      event.preventDefault();
      toggle();
    }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [toggle]);

  return (
    <Toggle.Provider value={{ collapsed, toggle }}>
      <aside
        data-collapsed={collapsed}
        className={cn(
          "group/sidebar sticky top-0 hidden h-svh shrink-0 border-r bg-sidebar text-sidebar-foreground md:block",
          collapsed ? "w-14" : "w-sidebar",
        )}
      >
        {children}
      </aside>
    </Toggle.Provider>
  );
}

// SidebarToggle is the button that collapses and opens the sidebar. It is only
// drawn inside the frame: the narrow menu is not collapsible.
export function SidebarToggle() {
  const state = useContext(Toggle);
  if (!state) return null;
  const { collapsed, toggle } = state;
  const Icon = collapsed ? PanelLeftOpen : PanelLeftClose;

  return (
    <button
      type="button"
      onClick={toggle}
      aria-pressed={collapsed}
      aria-keyshortcuts="Control+B Meta+B"
      title={collapsed ? "Expand sidebar (Ctrl+B)" : "Collapse sidebar (Ctrl+B)"}
      className="grid size-7 shrink-0 place-items-center rounded-md text-muted-foreground hover:bg-sidebar-accent hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
    >
      <Icon aria-hidden className="size-4" />
      <span className="sr-only">{collapsed ? "Expand sidebar" : "Collapse sidebar"}</span>
    </button>
  );
}
