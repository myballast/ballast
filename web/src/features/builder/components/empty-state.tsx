import Link from "next/link";

import { Button, buttonVariants } from "@/components/ui/button";
import { cn } from "@/lib/utils";

// EmptyState is what a canvas with no node says: where a scenario starts, and the
// other places its first requests come from.
export function EmptyState({
  projectId,
  readOnly,
  onAdd,
}: {
  projectId: string;
  readOnly: boolean;
  onAdd: () => void;
}) {
  const imports = [
    { from: "curl", label: "Import cURL" },
    { from: "har", label: "Import HAR" },
    { from: "openapi", label: "From OpenAPI" },
  ];

  return (
    <div className="absolute inset-0 z-[1] flex items-center justify-center p-6">
      <div className="flex max-w-sm flex-col items-center gap-3 rounded-xl border bg-card p-6 text-center">
        <div aria-hidden className="grid size-9 place-items-center rounded-lg border text-lg text-muted-foreground">
          +
        </div>
        <h2 className="text-base font-medium">{readOnly ? "This scenario has no node" : "Start with a request"}</h2>
        <p className="text-sm text-muted-foreground">
          {readOnly
            ? "Nothing has been drawn yet."
            : "Every scenario begins with one HTTP call. Chain extractions, conditions and thresholds from there."}
        </p>
        {!readOnly && (
          <>
            <Button onClick={onAdd}>Add HTTP request node</Button>
            <div className="flex flex-wrap justify-center gap-2">
              {imports.map((entry) => (
                <Link
                  key={entry.from}
                  href={`/projects/${projectId}/scenarios/import?from=${entry.from}`}
                  className={cn(buttonVariants({ variant: "outline", size: "sm" }))}
                >
                  {entry.label}
                </Link>
              ))}
            </div>
          </>
        )}
      </div>
    </div>
  );
}
