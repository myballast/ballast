// The theme of the application: light, dark, or whatever the system says. Dark is
// the default, and the choice is a cookie so that the server draws the page in the
// theme it will keep, with no flash of the other one.

export const themes = ["light", "dark", "system"] as const;
export type Theme = (typeof themes)[number];

export const themeCookie = "theme";

// parseTheme reads the cookie. Anything that is not one of the three is dark: a
// stale or hand-written value must not leave the page without a theme.
export function parseTheme(value: string | undefined): Theme {
  return themes.find((t) => t === value) ?? "dark";
}

export function isDark(theme: Theme, systemPrefersDark: boolean): boolean {
  return theme === "dark" || (theme === "system" && systemPrefersDark);
}

// What runs in the head, before the page is painted, when the theme follows the
// system: the server cannot know it, so the class is set here. Written out as text:
// it is an inline script, and it holds nothing a person typed.
export const systemThemeScript = `document.documentElement.classList.toggle("dark",window.matchMedia("(prefers-color-scheme: dark)").matches)`;
