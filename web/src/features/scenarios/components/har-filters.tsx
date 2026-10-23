import { hostCounts, kindCounts, requestKinds } from "../har-filter";
import type { CollectionRequest, RequestKind } from "../types";

const select =
  "h-9 max-w-full rounded-md border bg-background px-3 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring";

// HARFilters narrows a recording to what is worth testing: one host, since a
// scenario sends its load to one target, and the kinds of request, API calls
// first. Pages, images and files are what a browser fetches around the calls.
export function HARFilters({
  requests,
  host,
  kinds,
  onHost,
  onKind,
}: {
  requests: CollectionRequest[];
  host: string;
  kinds: ReadonlySet<RequestKind>;
  onHost: (host: string) => void;
  onKind: (kind: RequestKind) => void;
}) {
  const counts = kindCounts(requests, host);

  return (
    <div className="flex flex-col gap-4 rounded-xl border bg-card p-4">
      <label className="flex max-w-xl flex-col gap-1.5 text-sm">
        Host
        <select value={host} onChange={(event) => onHost(event.target.value)} className={select}>
          {hostCounts(requests).map((entry) => (
            <option key={entry.host} value={entry.host}>
              {entry.host || "No host"} · {entry.api} API calls, {entry.total} in all
            </option>
          ))}
        </select>
        <span className="text-xs text-muted-foreground">
          A scenario sends its load to one target. The base URL of the environment you run it on replaces this host.
        </span>
      </label>

      <fieldset className="flex flex-wrap gap-x-5 gap-y-2 text-sm">
        <legend className="pb-1">What to show</legend>
        {requestKinds.map((kind) => {
          const count = counts.get(kind.id) ?? 0;
          return (
            <label key={kind.id} className="flex items-center gap-2">
              <input
                type="checkbox"
                checked={kinds.has(kind.id)}
                disabled={count === 0}
                onChange={() => onKind(kind.id)}
                className="accent-brand"
              />
              <span className={count === 0 ? "text-muted-foreground" : undefined}>
                {kind.label} <span className="num text-xs text-muted-foreground">{count}</span>
              </span>
            </label>
          );
        })}
      </fieldset>
    </div>
  );
}
