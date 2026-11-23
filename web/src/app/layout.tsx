import type { Metadata, Viewport } from "next";
import { cookies } from "next/headers";
import localFont from "next/font/local";

import { parseTheme, systemThemeScript, themeCookie } from "@/features/theme/theme";

import { Providers } from "./providers";
import "@/styles/globals.css";
import { themeColors } from "@/styles/theme-colors";

// Two weights and no more: 400 for text, 500 for emphasis. The lint script
// refuses any other.
//
// The fonts are files of this repository (src/app/fonts) and not a request to Google:
// a build that has to reach a third party to finish fails whenever that party cannot
// be reached — which is what a locked-down build machine does — and talks to someone
// about this project every time it runs. Each file is a variable font holding both
// weights, restricted here to the two that are used.
const sans = localFont({
  src: "./fonts/dm-sans-latin.woff2",
  variable: "--font-dm-sans",
  weight: "400 500",
  display: "swap",
});
const mono = localFont({
  src: "./fonts/jetbrains-mono-latin.woff2",
  variable: "--font-jetbrains-mono",
  weight: "400 500",
  display: "swap",
});

export const metadata: Metadata = {
  title: "Ballast",
  description: "Load testing for the applications you own.",
};

export const viewport: Viewport = {
  // The browser's own bars follow the system; the page's theme may differ when it
  // was chosen by hand, which is the one thing a static value cannot say.
  themeColor: [
    { media: "(prefers-color-scheme: light)", color: themeColors.light.background },
    { media: "(prefers-color-scheme: dark)", color: themeColors.background },
  ],
};

// Dark by default. The theme is a cookie, read here so that the page is drawn in
// it from the server; "system" is the one case the server cannot know, and a script
// in the head settles it before anything is painted.
export default async function RootLayout({ children }: LayoutProps<"/">) {
  const theme = parseTheme((await cookies()).get(themeCookie)?.value);

  return (
    <html
      lang="en"
      suppressHydrationWarning
      className={`${theme === "dark" ? "dark " : ""}${sans.variable} ${mono.variable} h-full antialiased`}
    >
      {theme === "system" && (
        <head>
          <script dangerouslySetInnerHTML={{ __html: systemThemeScript }} />
        </head>
      )}
      {/* Some browser extensions (a shortcut helper writes cz-shortcut-listen) add
          attributes to <body> before React loads. They are not ours to fix, and this
          silences the warning for the <body> element's own attributes only. */}
      <body suppressHydrationWarning className="flex min-h-full flex-col">
        <Providers>{children}</Providers>
      </body>
    </html>
  );
}
