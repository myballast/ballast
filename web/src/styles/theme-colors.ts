// The two colours that must be written as values rather than tokens: the web
// manifest and the browser's theme colour are read outside any stylesheet, so
// CSS variables mean nothing there. They mirror --background and --brand in
// globals.css (dark theme). This is the one file scripts/check-palette.mjs
// lets write a colour in hex outside globals.css.
export const themeColors = {
  background: "#0b0b0f",
  brand: "#8b5cf6",
} as const;
