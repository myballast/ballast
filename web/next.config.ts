import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  // The image copies .next/standalone, which only exists in this mode: Next
  // then emits a server and the subset of node_modules it actually loads,
  // instead of the whole tree.
  output: "standalone",

  // A server action receives the text of uploaded files: a specification, or a
  // Postman collection with its environment, each up to 5 MB. The default of
  // 1 MB would refuse most real ones before the API ever saw them; the API still
  // enforces its own limit on each file.
  experimental: {
    serverActions: {
      bodySizeLimit: "24mb",
    },
  },
};

export default nextConfig;
