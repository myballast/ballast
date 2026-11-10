"use client";

import { Monitor, Moon, Sun, type LucideIcon } from "lucide-react";
import { useEffect, useState } from "react";

import { cn } from "@/lib/utils";
import { isDark, themeCookie, themes, type Theme } from "@/features/theme/theme";

const choices: Record<Theme, { label: string; icon: LucideIcon }> = {
  light: { label: "Light", icon: Sun },
  dark: { label: "Dark", icon: Moon },
  system: { label: "System", icon: Monitor },
};

// Remembered for the next page, and shown on this one at once.
function save(theme: Theme) {
  document.cookie = `${themeCookie}=${theme}; path=/; max-age=31536000; samesite=lax`;
  apply(theme);
}

function apply(theme: Theme) {
  const dark = isDark(theme, window.matchMedia("(prefers-color-scheme: dark)").matches);
  document.documentElement.classList.toggle("dark", dark);
}

// ThemeToggle is the choice of theme: light, dark, or the system's. The choice is a
// cookie, so that the next page is drawn in it from the server, and the class of the
// page changes at once. With "system" it follows the system as that changes.
export function ThemeToggle({ initial }: { initial: Theme }) {
  const [theme, setTheme] = useState<Theme>(initial);

  useEffect(() => {
    if (theme !== "system") return;
    const query = window.matchMedia("(prefers-color-scheme: dark)");
    const follow = () => apply("system");
    query.addEventListener("change", follow);
    return () => query.removeEventListener("change", follow);
  }, [theme]);

  function choose(next: Theme) {
    setTheme(next);
    save(next);
  }

  return (
    <div role="radiogroup" aria-label="Theme" className="flex rounded-lg border p-0.5">
      {themes.map((t) => {
        const { label, icon: Icon } = choices[t];
        return (
          <button
            key={t}
            type="button"
            role="radio"
            aria-checked={theme === t}
            title={label}
            onClick={() => choose(t)}
            className={cn(
              "grid size-7 place-items-center rounded-md text-muted-foreground hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none",
              theme === t && "bg-muted text-foreground",
            )}
          >
            <Icon aria-hidden className="size-4" />
            <span className="sr-only">{label}</span>
          </button>
        );
      })}
    </div>
  );
}
