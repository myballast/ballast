// The colours that must be written as values rather than tokens: the web
// manifest and the browser's theme colour are read outside any stylesheet, so
// CSS variables mean nothing there. They mirror --background and --brand in
// globals.css. This is the one file scripts/check-palette.mjs lets write a
// colour in hex outside globals.css.
export const themeColors = {
  background: "#13131a",
  brand: "#7b4ff0",
  light: { background: "#f7f7fb" },
} as const;
