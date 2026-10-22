import { endpointKey, type EndpointGroup } from "../import-selection";

// EndpointList shows the endpoints of a specification under their tags, each
// with a box to tick. The method and the path are the template as the file
// wrote it, in the monospace the charter keeps for addresses.
export function EndpointList({
  groups,
  chosen,
  full,
  onToggle,
  onToggleGroup,
}: {
  groups: EndpointGroup[];
  chosen: ReadonlySet<string>;
  // True when no more can be ticked: a scenario holds a limited number.
  full: boolean;
  onToggle: (key: string) => void;
  onToggleGroup: (group: EndpointGroup) => void;
}) {
  if (groups.length === 0) {
    return <p className="rounded-xl border border-dashed px-4 py-8 text-center text-sm text-muted-foreground">No endpoint matches.</p>;
  }

  return (
    <div className="flex flex-col gap-4">
      {groups.map((group) => {
        const tickedHere = group.endpoints.filter((endpoint) => chosen.has(endpointKey(endpoint))).length;
        const allTicked = tickedHere === group.endpoints.length;

        return (
          <fieldset key={group.label} className="flex flex-col rounded-xl border">
            <legend className="sr-only">{group.label}</legend>

            <div className="flex items-center justify-between gap-3 border-b px-4 py-2.5">
              <h2 className="text-sm font-medium">
                {group.label}{" "}
                <span className="num text-xs font-normal text-muted-foreground">
                  {tickedHere} / {group.endpoints.length}
                </span>
              </h2>
              <button
                type="button"
                onClick={() => onToggleGroup(group)}
                className="text-xs text-muted-foreground hover:text-foreground hover:underline"
              >
                {allTicked ? "Clear group" : "Select group"}
              </button>
            </div>

            <ul className="flex flex-col divide-y">
              {group.endpoints.map((endpoint) => {
                const key = endpointKey(endpoint);
                const ticked = chosen.has(key);

                return (
                  <li key={key}>
                    <label className="flex cursor-pointer items-start gap-3 px-4 py-2.5 text-sm hover:bg-muted/40 has-[:focus-visible]:ring-2 has-[:focus-visible]:ring-ring">
                      <input
                        type="checkbox"
                        checked={ticked}
                        disabled={full && !ticked}
                        onChange={() => onToggle(key)}
                        className="mt-1 accent-brand"
                      />
                      <span className="flex min-w-0 flex-col gap-0.5">
                        <span className="data flex flex-wrap gap-x-2 break-all">
                          <span className="w-14 shrink-0 text-muted-foreground">{endpoint.method}</span>
                          <span>{endpoint.path}</span>
                        </span>
                        <span className="flex flex-wrap gap-x-3 text-xs text-muted-foreground">
                          {endpoint.summary && <span>{endpoint.summary}</span>}
                          {endpoint.has_body && <span>{endpoint.body_too_long ? "body: example too long" : "has a body"}</span>}
                        </span>
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
