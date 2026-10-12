import type { Metadata, Viewport } from "next";
import { DM_Sans, JetBrains_Mono } from "next/font/google";

import { Providers } from "./providers";
import "@/styles/globals.css";
import { themeColors } from "@/styles/theme-colors";

// Two weights and no more: 400 for text, 500 for emphasis. The lint script
// refuses any other.
const sans = DM_Sans({
  variable: "--font-dm-sans",
  subsets: ["latin"],
  weight: ["400", "500"],
});
const mono = JetBrains_Mono({
  variable: "--font-jetbrains-mono",
  subsets: ["latin"],
  weight: ["400", "500"],
});

export const metadata: Metadata = {
  title: "Ballast",
  description: "Load testing for the applications you own.",
};

export const viewport: Viewport = {
  themeColor: themeColors.background,
  colorScheme: "dark",
};

// Dark by default: the class is set here rather than followed from the system.
export default function RootLayout({ children }: LayoutProps<"/">) {
  return (
    <html
      lang="en"
      className={`dark ${sans.variable} ${mono.variable} h-full antialiased`}
    >
      <body className="flex min-h-full flex-col">
        <Providers>{children}</Providers>
      </body>
    </html>
  );
}
