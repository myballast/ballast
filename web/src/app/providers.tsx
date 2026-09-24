"use client";

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useState } from "react";

// One client per browser session. Created in state rather than at module
// level so that two requests rendered on the same server never share a cache.
export function Providers({ children }: { children: React.ReactNode }) {
  const [client] = useState(() => new QueryClient());

  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}
