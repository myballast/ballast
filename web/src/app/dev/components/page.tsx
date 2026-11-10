import { cookies } from "next/headers";
import { notFound } from "next/navigation";

import { ThemeToggle } from "@/components/layout/theme-toggle";
import { parseTheme, themeCookie } from "@/features/theme/theme";

import { ComponentReference } from "@/features/styleguide/components/component-reference";

// The component reference exists in development only. In a production build
// this page is a 404, and the proxy lets it through without a session so that
// it answers 404 rather than a redirect to sign in.
export default async function ComponentsPage() {
  if (process.env.NODE_ENV === "production") {
    notFound();
  }

  const theme = parseTheme((await cookies()).get(themeCookie)?.value);

  return (
    <main className="mx-auto w-full max-w-6xl px-4 py-page-y md:px-page-x">
      {/* Every component in either theme: the same switch as the header's. */}
      <div className="mb-6 flex items-center justify-end gap-3 text-xs text-muted-foreground">
        Theme
        <ThemeToggle initial={theme} />
      </div>
      <ComponentReference />
    </main>
  );
}
