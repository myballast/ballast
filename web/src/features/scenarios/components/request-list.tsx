import type { RequestGroup } from "../collection-selection";

// RequestList shows the requests of a collection under their folders, in the
// order the collection runs them, each with a box to tick. A request that cannot
// become part of a scenario cannot be ticked, and says why underneath: nothing
// that was left out is left out silently.
export function RequestList({
  groups,
  chosen,
  full,
  onToggle,
  onToggleGroup,
}: {
  groups: RequestGroup[];
  chosen: ReadonlySet<number>;
  // True when no more can be ticked: a scenario holds a limited number.
  full: boolean;
  onToggle: (position: number) => void;
  onToggleGroup: (group: RequestGroup) => void;
}) {
  if (groups.length === 0) {
    return (
      <p className="rounded-xl border border-dashed px-4 py-8 text-center text-sm text-muted-foreground">
        No request matches.
      </p>
    );
  }

  return (
    <div className="flex flex-col gap-4">
      {groups.map((group, groupIndex) => {
        const usable = group.requests.filter((request) => !request.left_out);
        const tickedHere = usable.filter((request) => chosen.has(request.position)).length;
        const allTicked = usable.length > 0 && tickedHere === usable.length;

        return (
          <fieldset key={`${groupIndex}-${group.label}`} className="flex flex-col rounded-xl border">
            <legend className="sr-only">{group.label}</legend>

            <div className="flex items-center justify-between gap-3 border-b px-4 py-2.5">
              <h2 className="text-sm font-medium">
                {group.label}{" "}
                <span className="num text-xs font-normal text-muted-foreground">
                  {tickedHere} / {usable.length}
                </span>
              </h2>
              {usable.length > 0 && (
                <button
                  type="button"
                  onClick={() => onToggleGroup(group)}
                  className="text-xs text-muted-foreground hover:text-foreground hover:underline"
                >
                  {allTicked ? "Clear group" : "Select group"}
                </button>
              )}
            </div>

            <ul className="flex flex-col divide-y">
              {group.requests.map((request) => {
                const ticked = chosen.has(request.position);

                return (
                  <li key={request.position}>
                    <label className="flex cursor-pointer items-start gap-3 px-4 py-2.5 text-sm hover:bg-muted/40 has-[:focus-visible]:ring-2 has-[:focus-visible]:ring-ring">
                      <input
                        type="checkbox"
                        checked={ticked}
                        disabled={request.left_out || (full && !ticked)}
                        onChange={() => onToggle(request.position)}
                        className="mt-1 accent-brand"
                      />
                      <span className="flex min-w-0 flex-col gap-0.5">
                        <span className="flex flex-wrap gap-x-2">
                          <span className="font-medium">{request.name}</span>
                          {request.left_out && <span className="text-xs text-muted-foreground">left out</span>}
                        </span>
                        <span className="data flex gap-2 break-all text-xs">
                          <span className="w-14 shrink-0 text-muted-foreground">{request.method}</span>
                          <span>{request.path || "—"}</span>
                        </span>
                        {request.findings.map((finding) => (
                          <span key={finding} className="text-xs text-muted-foreground">
                            {finding}
                          </span>
                        ))}
                      </span>
                    </label>
                  </li>
                );
              })}
            </ul>
          </fieldset>
        );
      })}
    </div>
  );
}
