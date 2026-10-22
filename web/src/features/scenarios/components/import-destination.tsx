const field =
  "h-9 rounded-md border bg-background px-3 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring";

// ImportDestination asks what to do with the chosen requests: make a scenario of
// them, keep them in the project's catalog, or both. It is the part of an import
// screen that does not depend on where the requests came from.
export function ImportDestination({
  noun,
  makeScenario,
  onMakeScenario,
  addToCatalog,
  onAddToCatalog,
  name,
  onName,
  source,
  onSource,
}: {
  // What the things being imported are called: "endpoint" or "request".
  noun: string;
  makeScenario: boolean;
  onMakeScenario: (value: boolean) => void;
  addToCatalog: boolean;
  onAddToCatalog: (value: boolean) => void;
  name: string;
  onName: (value: string) => void;
  source: string;
  onSource: (value: string) => void;
}) {
  return (
    <>
      <fieldset className="flex flex-col gap-2 text-sm">
        <legend className="pb-1">What to do with them</legend>
        <label className="flex items-center gap-2">
          <input
            type="checkbox"
            checked={makeScenario}
            onChange={(event) => onMakeScenario(event.target.checked)}
            className="accent-brand"
          />
          Create a scenario, one request per {noun}
        </label>
        <label className="flex items-center gap-2">
          <input
            type="checkbox"
            checked={addToCatalog}
            onChange={(event) => onAddToCatalog(event.target.checked)}
            className="accent-brand"
          />
          Add them to the project&apos;s endpoints, to pick from when writing scenarios
        </label>
      </fieldset>

      {makeScenario && (
        <label className="flex max-w-xl flex-col gap-1.5 text-sm">
          Scenario name
          <input
            required
            maxLength={120}
            value={name}
            onChange={(event) => onName(event.target.value)}
            className={field}
          />
        </label>
      )}

      {addToCatalog && (
        <label className="flex max-w-xl flex-col gap-1.5 text-sm">
          Source
          <input
            required
            maxLength={120}
            value={source}
            onChange={(event) => onSource(event.target.value)}
            className={field}
          />
          <span className="text-xs text-muted-foreground">
            Importing the same source again replaces what it brought before. Other sources are left alone.
          </span>
        </label>
      )}
    </>
  );
}
