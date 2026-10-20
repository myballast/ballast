import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  // The image copies .next/standalone, which only exists in this mode: Next
  // then emits a server and the subset of node_modules it actually loads,
  // instead of the whole tree.
  output: "standalone",
};

export default nextConfig;
