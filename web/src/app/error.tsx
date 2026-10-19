"use client";

import { SegmentError } from "@/components/segment-error";

// The last net: a failure outside the application's frame — the sign-in page, an
// invitation — where there is no sidebar to keep, only a page to offer again.
export default function RootError(props: { error: Error & { digest?: string }; reset: () => void }) {
  return (
    <main className="flex flex-1 items-center justify-center px-4 py-16">
      <div className="w-full max-w-md">
        <SegmentError {...props} />
      </div>
    </main>
  );
}
