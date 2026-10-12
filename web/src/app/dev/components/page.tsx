import { notFound } from "next/navigation";

import { ComponentReference } from "@/features/styleguide/components/component-reference";

// The component reference exists in development only. In a production build
// this page is a 404, and the proxy lets it through without a session so that
// it answers 404 rather than a redirect to sign in.
export default function ComponentsPage() {
  if (process.env.NODE_ENV === "production") {
    notFound();
  }

  return (
    <main className="mx-auto w-full max-w-6xl px-4 py-page-y md:px-page-x">
      <ComponentReference />
    </main>
  );
}
