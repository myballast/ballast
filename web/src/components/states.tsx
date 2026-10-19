import { Inbox, LoaderCircle, RotateCw, TriangleAlert, type LucideIcon } from "lucide-react";

import { Button } from "@/components/ui/button";

// LoadingState stands in for content that is on its way. The spinner is
// neutral: loading is not a status of a run.
export function LoadingState({ label = "Loading…" }: { label?: string }) {
  return (
    <div role="status" className="flex items-center gap-2 py-10 text-sm text-muted-foreground">
      <LoaderCircle aria-hidden className="size-4 animate-spin" />
      {label}
    </div>
  );
}

// EmptyState says that there is nothing here yet, and what would put
// something here. The action, if any, is the page's to give.
export function EmptyState({
  title,
  description,
  icon: Icon = Inbox,
  action,
}: {
  title: string;
  description?: string;
  icon?: LucideIcon;
  action?: React.ReactNode;
}) {
  return (
    <div className="flex flex-col items-center gap-3 rounded-xl border border-dashed px-6 py-12 text-center">
      <Icon aria-hidden className="size-6 text-muted-foreground" />
      <div className="flex flex-col gap-1">
        <p className="text-sm font-medium">{title}</p>
        {description && <p className="max-w-sm text-sm text-muted-foreground">{description}</p>}
      </div>
      {action}
    </div>
  );
}

// ErrorState says that a page could not be drawn, and offers the one thing that
// might help: trying again. It never says what the server said — an error from
// below can name a table or an address — only that something went wrong, and
// whether it is worth retrying.
//
// Neutral: the red of a failed run is for runs, and a failed request is not one.
export function ErrorState({
  title = "Something went wrong",
  description = "The page could not be loaded. Trying again usually works; if it does not, the problem is on our side and not in what you asked for.",
  onRetry,
}: {
  title?: string;
  description?: string;
  onRetry?: () => void;
}) {
  return (
    <div role="alert" className="flex flex-col items-center gap-3 rounded-xl border border-dashed px-6 py-12 text-center">
      <TriangleAlert aria-hidden className="size-6 text-muted-foreground" />
      <div className="flex flex-col gap-1">
        <p className="text-sm font-medium">{title}</p>
        <p className="max-w-sm text-sm text-muted-foreground">{description}</p>
      </div>
      {onRetry && (
        <Button variant="outline" onClick={onRetry}>
          <RotateCw aria-hidden />
          Try again
        </Button>
      )}
    </div>
  );
}
