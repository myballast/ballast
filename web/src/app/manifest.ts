import type { MetadataRoute } from "next";

import { themeColors } from "@/styles/theme-colors";

export default function manifest(): MetadataRoute.Manifest {
  return {
    name: "Ballast",
    short_name: "Ballast",
    description: "Load testing for the applications you own.",
    start_url: "/",
    display: "standalone",
    background_color: themeColors.background,
    theme_color: themeColors.background,
    icons: [
      { src: "/icon.svg", sizes: "any", type: "image/svg+xml" },
      { src: "/apple-icon.png", sizes: "180x180", type: "image/png" },
    ],
  };
}
