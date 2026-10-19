import { cn } from "@/lib/utils";

// Skeleton is a block standing where content will be. It is drawn in a surface
// tone with a slow pulse — no colour, since loading is not a status — and
// is hidden from assistive technology: the page announces what is loading once,
// with the label of SkeletonPage, not once per block.
export function Skeleton({ className }: { className?: string }) {
  return <div aria-hidden className={cn("animate-pulse rounded-md bg-muted", className)} />;
}

// SkeletonPage is the wrapper of a loading page: one status for a screen reader,
// and the shapes of what is coming for everybody else. The label says what is
// loading, as the line of text it replaces did.
export function SkeletonPage({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div role="status" aria-live="polite" aria-busy="true" className="flex flex-col gap-8">
      <span className="sr-only">{label}</span>
      {children}
    </div>
  );
}

// The head of every page: a title, and a line under it.
export function SkeletonHeading() {
  return (
    <div className="flex flex-col gap-2.5">
      <Skeleton className="h-8 w-56" />
      <Skeleton className="h-3.5 w-80 max-w-full" />
    </div>
  );
}

// A grid of cards, as the project list is.
export function SkeletonCards({ count = 6 }: { count?: number }) {
  return (
    <ul className="grid grid-cols-1 gap-3.5 md:grid-cols-2 xl:grid-cols-3">
      {Array.from({ length: count }, (_, i) => (
        <li key={i} className="flex flex-col gap-3 rounded-xl border bg-card p-4">
          <Skeleton className="h-4 w-40" />
          <Skeleton className="h-3 w-24" />
          <div className="flex gap-1.5">
            <Skeleton className="h-5 w-10" />
            <Skeleton className="h-5 w-10" />
            <Skeleton className="h-5 w-10" />
          </div>
          <Skeleton className="h-3 w-52 max-w-full" />
        </li>
      ))}
    </ul>
  );
}

// Rows in a bordered table, as the runs, environments and members are.
export function SkeletonTable({ rows = 6 }: { rows?: number }) {
  return (
    <div className="overflow-hidden rounded-xl border bg-card">
      <div className="border-b px-4 py-3">
        <Skeleton className="h-3 w-1/3" />
      </div>
      {Array.from({ length: rows }, (_, i) => (
        <div key={i} className="flex items-center gap-4 border-b px-4 py-3.5 last:border-b-0">
          <Skeleton className="h-4 w-24" />
          <Skeleton className="h-4 flex-1" />
          <Skeleton className="hidden h-4 w-20 sm:block" />
          <Skeleton className="hidden h-4 w-16 md:block" />
        </div>
      ))}
    </div>
  );
}

// A row of figures, as the project page and the run page open with.
export function SkeletonTiles({ count = 4 }: { count?: number }) {
  return (
    <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
      {Array.from({ length: count }, (_, i) => (
        <div key={i} className="flex flex-col gap-3 rounded-xl border bg-card p-4">
          <Skeleton className="h-3 w-20" />
          <Skeleton className="h-7 w-24" />
        </div>
      ))}
    </div>
  );
}

// A block of body, as a script, a form or a panel is.
export function SkeletonPanel({ height = "h-56" }: { height?: string }) {
  return <Skeleton className={cn("w-full rounded-xl", height)} />;
}
