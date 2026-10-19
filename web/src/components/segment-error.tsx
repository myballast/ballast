"use client";

import { useRouter } from "next/navigation";
import { startTransition, useEffect } from "react";

import { ErrorState } from "@/components/states";

// SegmentError is the body of an error.tsx: the page replaced by an error state
// with "Try again", inside the layout of the segment it sits in — so the sidebar
// and the breadcrumb stay, and the way out is where it always was.
//
// The error goes to the console for whoever is debugging and no further: what an
// API or a database said is not for the person looking at the page.
export function SegmentError({ error, reset }: { error: Error & { digest?: string }; reset: () => void }) {
  const router = useRouter();

  useEffect(() => {
    console.error(error);
  }, [error]);

  // A page that failed on the server failed there: rendering the segment again in
  // the browser would show the same error. The server has to be asked again, and
  // only then is the boundary reset.
  const retry = () =>
    startTransition(() => {
      router.refresh();
      reset();
    });

  return <ErrorState onRetry={retry} />;
}
