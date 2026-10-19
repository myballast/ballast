"use client";

import { useRouter } from "next/navigation";
import { useCallback, useEffect, useRef, useState } from "react";

import { Button } from "@/components/ui/button";

// useUnsavedGuard stops somebody from walking away from changes that were not
// saved, and asks first. It returns the dialog it asks with, to be rendered once.
//
// Three ways out are covered, because they are three different things to a page
// that does not reload. Closing or reloading the tab is the browser's: it raises
// its own prompt, which no page can replace. Following a link inside the
// application, and the Back button, are the application's own: a click on a link
// is caught before it navigates, and Back is caught by standing a step in front of
// it in the history. Neither can be blocked by an API Next.js offers, which is why
// this is a guard on the events and not a hook of the router.
export function useUnsavedGuard(dirty: boolean) {
  const router = useRouter();
  const dialog = useRef<HTMLDialogElement>(null);
  // Where "Leave" goes: an address, or back in the history.
  const [destination, setDestination] = useState<{ href: string } | { back: true } | null>(null);
  // Set while leaving on purpose, so that the guard does not catch the exit it
  // has just been told to allow.
  const leaving = useRef(false);
  const sentinel = useRef(false);

  // The browser's own prompt, for the tab.
  useEffect(() => {
    if (!dirty) return;

    const warn = (event: BeforeUnloadEvent) => {
      if (leaving.current) return;
      event.preventDefault();
    };
    window.addEventListener("beforeunload", warn);
    return () => window.removeEventListener("beforeunload", warn);
  }, [dirty]);

  // A link followed inside the application.
  useEffect(() => {
    if (!dirty) return;

    const intercept = (event: MouseEvent) => {
      if (leaving.current || event.defaultPrevented || event.button !== 0) return;
      if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;

      const link = (event.target as Element | null)?.closest("a[href]");
      if (!(link instanceof HTMLAnchorElement) || link.target === "_blank" || link.hasAttribute("download")) return;

      const next = new URL(link.href, window.location.href);
      // Another address on this site; the same page, or a jump within it, is not
      // leaving.
      if (next.origin !== window.location.origin) return;
      if (next.pathname === window.location.pathname && next.search === window.location.search) return;

      event.preventDefault();
      event.stopPropagation();
      setDestination({ href: next.pathname + next.search + next.hash });
    };

    // Captured: before Next's own handler on the link sees it.
    document.addEventListener("click", intercept, true);
    return () => document.removeEventListener("click", intercept, true);
  }, [dirty]);

  // Back. While there are changes, one more step is stood in the history in front
  // of the way out: Back lands on it, the page asks, and stays — or, on "Leave",
  // goes two steps back to where it was going.
  useEffect(() => {
    if (!dirty) return;

    // Keeps the state Next keeps there, or Next would not know the entry.
    window.history.pushState(window.history.state, "", window.location.href);
    sentinel.current = true;

    const back = () => {
      if (leaving.current) return;
      window.history.pushState(window.history.state, "", window.location.href);
      setDestination({ back: true });
    };
    window.addEventListener("popstate", back);
    return () => window.removeEventListener("popstate", back);
  }, [dirty]);

  useEffect(() => {
    if (destination) dialog.current?.showModal();
    else dialog.current?.close();
  }, [destination]);

  const leave = useCallback(() => {
    const target = destination;
    leaving.current = true;
    setDestination(null);

    if (!target) return;
    if ("href" in target) router.push(target.href);
    else window.history.go(sentinel.current ? -2 : -1);
  }, [destination, router]);

  const guard = (
    <dialog
      ref={dialog}
      aria-labelledby="unsaved-title"
      onClose={() => setDestination(null)}
      className="m-auto w-[min(26rem,calc(100vw-2rem))] rounded-xl border bg-card p-0 text-left text-foreground backdrop:bg-background/70"
    >
      <div className="flex flex-col gap-4 p-5">
        <h2 id="unsaved-title" className="text-lg font-normal tracking-tight">
          Leave without saving?
        </h2>
        <p className="text-sm text-muted-foreground">
          You changed this scenario and have not saved it. Leaving now throws those changes away.
        </p>
        <div className="flex gap-2">
          <Button autoFocus onClick={() => setDestination(null)}>
            Stay on this page
          </Button>
          <Button variant="outline" onClick={leave}>
            Leave without saving
          </Button>
        </div>
      </div>
    </dialog>
  );

  return guard;
}
