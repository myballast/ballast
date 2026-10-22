import { groupFindings } from "../collection-selection";
import type { ImportFinding } from "../types";

// ImportReport lists what an import could not carry over, request by request. An
// import that converted everything says so, rather than showing an empty box that
// could be read as a report that failed to load.
export function ImportReport({ findings }: { findings: ImportFinding[] }) {
  const groups = groupFindings(findings);

  if (groups.length === 0) {
    return (
      <section className="flex flex-col gap-1 rounded-xl border p-4 text-sm">
        <h2 className="font-medium">Import report</h2>
        <p className="text-muted-foreground">Everything in the chosen requests was converted.</p>
      </section>
    );
  }

  return (
    <section className="flex flex-col gap-3 rounded-xl border p-4 text-sm">
      <h2 className="font-medium">Import report</h2>
      <p className="text-muted-foreground">
        What follows was not converted, or was changed. Nothing is left out without being listed here.
      </p>
      <ul className="flex flex-col gap-3">
        {groups.map((group) => (
          <li key={group.request || "collection"} className="flex flex-col gap-1">
            <span className="font-medium">{group.request || "The collection"}</span>
            <ul className="flex flex-col gap-0.5 text-muted-foreground">
              {group.messages.map((message) => (
                <li key={message}>{message}</li>
              ))}
            </ul>
          </li>
        ))}
      </ul>
    </section>
  );
}
