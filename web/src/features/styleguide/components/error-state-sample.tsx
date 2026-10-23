"use client";

import { ErrorState } from "@/components/states";

// ErrorState takes a function for its button, and a page of the reference is a
// server component, which cannot hand one over: this is the client side of that.
export function ErrorStateSample() {
  return <ErrorState onRetry={() => {}} />;
}
