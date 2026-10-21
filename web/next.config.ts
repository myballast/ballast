import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  // The image copies .next/standalone, which only exists in this mode: Next
  // then emits a server and the subset of node_modules it actually loads,
  // instead of the whole tree.
  output: "standalone",

  // A server action receives the text of an uploaded OpenAPI specification,
  // which the API accepts up to 5 MB. The default of 1 MB would refuse most
  // real ones before the API ever saw them; the API still enforces its own
  // limit on the file itself.
  experimental: {
    serverActions: {
      bodySizeLimit: "12mb",
    },
  },
};

export default nextConfig;
