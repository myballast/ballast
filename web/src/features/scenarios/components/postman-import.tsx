"use client";

import { FileUp, Info, Search } from "lucide-react";
import Link from "next/link";
import { useState, useTransition } from "react";

import { Button, buttonVariants } from "@/components/ui/button";
import { importCollectionToCatalog } from "@/features/endpoints/actions";

import {
  chosenPositions,
  firstChoice,
  groupByFolder,
  limitFor,
  toggleGroup,
  type RequestGroup,
} from "../collection-selection";
import { importPostman, previewPostman } from "../import-actions";
import { maxSpecBytes } from "../import-selection";
import type { CollectionPreview, DetectedSecret, ImportFinding } from "../types";
import { ImportDestination } from "./import-destination";
import { ImportReport } from "./import-report";
import { RequestList } from "./request-list";
import { SecretsPanel } from "./secrets-panel";

const field =
  "h-9 rounded-md border bg-background px-3 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring";

type Target = { id: string; name: string };

// The files, kept as text: the preview stores nothing on the server, so the import
// sends them again.
type Loaded = { collection: string; environment: string; preview: CollectionPreview };

// What an import leaves on screen: the report, the secrets to create, and where
// to go next. The person reads it before leaving, because the secrets are typed here.
type Done = {
  scenarioId?: string;
  catalogCount?: number;
  secrets: DetectedSecret[];
  findings: ImportFinding[];
};

function Notice({ children }: { children: React.ReactNode }) {
  return (
    <p role="status" className="flex items-start gap-2 rounded-lg border bg-card px-3 py-2 text-xs text-muted-foreground">
      <Info aria-hidden className="mt-0.5 size-3.5 shrink-0" />
      <span>{children}</span>
    </p>
  );
}

// PostmanImport takes a Postman collection, and an environment file if there is
// one, in two steps: pick the files, then tick the requests. The requests keep the
// order of the collection, which is a real journey, and what could not be carried
// over is said request by request.
export function PostmanImport({
  projectId,
  environments,
  canManageEnvironments,
}: {
  projectId: string;
  environments: Target[];
  canManageEnvironments: boolean;
}) {
  const [environmentFile, setEnvironmentFile] = useState<{ name: string; text: string } | null>(null);
  const [loaded, setLoaded] = useState<Loaded | null>(null);
  const [chosen, setChosen] = useState<ReadonlySet<number>>(new Set());
  const [search, setSearch] = useState("");
  const [name, setName] = useState("");
  const [makeScenario, setMakeScenario] = useState(true);
  const [addToCatalog, setAddToCatalog] = useState(false);
  const [source, setSource] = useState("");
  const [error, setError] = useState("");
  const [done, setDone] = useState<Done | null>(null);
  const [pending, startTransition] = useTransition();

  function tooLarge(file: File): boolean {
    if (file.size <= maxSpecBytes) return false;
    setError(`${file.name} is larger than 5 MB, the most Ballast reads.`);
    return true;
  }

  async function chooseEnvironment(file: File) {
    setError("");
    if (tooLarge(file)) return;
    setEnvironmentFile({ name: file.name, text: await file.text() });
  }

  function readCollection(file: File) {
    setError("");
    if (tooLarge(file)) return;

    startTransition(async () => {
      const collection = await file.text();
      const environment = environmentFile?.text ?? "";
      const result = await previewPostman(projectId, collection, environment);

      if ("error" in result) {
        setError(result.error);
        return;
      }

      const title = (result.preview.name || file.name.replace(/\.[^.]+$/, "")).slice(0, 120);
      setLoaded({ collection, environment, preview: result.preview });
      setChosen(firstChoice(result.preview.requests));
      setSearch("");
      setName(title);
      setSource(title);
    });
  }

  if (done) {
    return <ImportDone projectId={projectId} done={done} environments={environments} canManage={canManageEnvironments} />;
  }

  if (loaded === null) {
    return (
      <CollectionPicker
        pending={pending}
        error={error}
        environmentName={environmentFile?.name ?? ""}
        onCollection={readCollection}
        onEnvironment={chooseEnvironment}
      />
    );
  }

  const { collection, environment, preview } = loaded;
  const groups = groupByFolder(preview.requests, search);
  const limit = limitFor(makeScenario);
  const full = chosen.size >= limit;

  function toggle(position: number) {
    setError("");
    const next = new Set(chosen);
    if (next.has(position)) next.delete(position);
    else next.add(position);
    setChosen(next);
  }

  function toggleWholeGroup(group: RequestGroup) {
    const next = toggleGroup(chosen, group);
    if (next.size > limit) {
      setError(`A scenario holds at most ${limit} requests.`);
      return;
    }
    setError("");
    setChosen(next);
  }

  function importChosen() {
    setError("");

    startTransition(async () => {
      const positions = chosenPositions(preview.requests, chosen);
      let catalogCount: number | undefined;
      let findings: ImportFinding[] = [];

      if (addToCatalog) {
        const added = await importCollectionToCatalog(projectId, source.trim(), collection, environment, positions);
        if (added.error) {
          setError(added.error);
          return;
        }
        catalogCount = added.count;
        findings = added.findings ?? [];
      }

      if (!makeScenario) {
        setDone({ catalogCount, secrets: [], findings });
        return;
      }

      const created = await importPostman(projectId, name.trim(), collection, environment, positions);
      if ("error" in created) {
        setError(
          addToCatalog
            ? `The requests were added to the catalog, but the scenario was not created: ${created.error}`
            : created.error,
        );
        return;
      }
      setDone({ scenarioId: created.scenarioId, catalogCount, secrets: created.secrets, findings: created.findings });
    });
  }

  return (
    <div className="flex flex-col gap-6">
      <section className="flex flex-wrap items-center justify-between gap-3">
        <p className="text-sm">
          <span className="font-medium">{preview.name || "Untitled collection"}</span>{" "}
          <span className="num text-muted-foreground">
            · {preview.requests.length} {preview.requests.length === 1 ? "request" : "requests"}
          </span>
        </p>
        <Button type="button" variant="ghost" onClick={() => setLoaded(null)}>
          Choose other files
        </Button>
      </section>

      <div className="flex flex-col gap-2">
        {preview.findings.map((finding) => (
          <Notice key={finding.message}>{finding.message}</Notice>
        ))}
        {preview.secrets.length > 0 && (
          <Notice>
            <span className="num">{preview.secrets.length}</span> secrets were found in these files. None of their values
            is copied into the scenario: it reads each from the environment, and you create them after the import.
          </Notice>
        )}
      </div>

      <label className="relative flex max-w-md items-center text-sm">
        <span className="sr-only">Search requests</span>
        <Search aria-hidden className="pointer-events-none absolute left-3 size-4 text-muted-foreground" />
        <input
          type="search"
          value={search}
          onChange={(event) => setSearch(event.target.value)}
          placeholder="Search by name, folder, path or method"
          className={`${field} w-full pl-9`}
        />
      </label>

      <RequestList groups={groups} chosen={chosen} full={full} onToggle={toggle} onToggleGroup={toggleWholeGroup} />

      <form
        className="flex flex-col gap-4 rounded-xl border bg-card p-4"
        onSubmit={(event) => {
          event.preventDefault();
          importChosen();
        }}
      >
        <ImportDestination
          noun="request"
          makeScenario={makeScenario}
          onMakeScenario={setMakeScenario}
          addToCatalog={addToCatalog}
          onAddToCatalog={setAddToCatalog}
          name={name}
          onName={setName}
          source={source}
          onSource={setSource}
        />

        {error && (
          <p role="alert" className="rounded-lg border bg-background px-3 py-2 text-sm text-muted-foreground">
            {error}
          </p>
        )}

        <div className="flex flex-wrap items-center gap-2">
          <Button
            type="submit"
            disabled={
              pending ||
              chosen.size === 0 ||
              (!makeScenario && !addToCatalog) ||
              (makeScenario && name.trim() === "") ||
              (addToCatalog && source.trim() === "")
            }
          >
            <FileUp aria-hidden />
            {pending ? "Importing…" : `Import ${chosen.size} ${chosen.size === 1 ? "request" : "requests"}`}
          </Button>
          <Link href={`/projects/${projectId}/scenarios`} className={buttonVariants({ variant: "ghost" })}>
            Cancel
          </Link>
        </div>
      </form>
    </div>
  );
}

// CollectionPicker is the first step: the environment file, if there is one, and
// then the collection, which starts the preview.
function CollectionPicker({
  pending,
  error,
  environmentName,
  onCollection,
  onEnvironment,
}: {
  pending: boolean;
  error: string;
  environmentName: string;
  onCollection: (file: File) => void;
  onEnvironment: (file: File) => void;
}) {
  return (
    <div className="flex max-w-xl flex-col gap-4">
      <label className="flex flex-col gap-1.5 text-sm">
        <span>
          Environment file <span className="text-muted-foreground">(optional)</span>
        </span>
        <input
          type="file"
          accept=".json,application/json"
          disabled={pending}
          className="text-xs"
          onChange={(event) => {
            const file = event.target.files?.[0];
            if (file) onEnvironment(file);
          }}
        />
        <span className="text-xs text-muted-foreground">
          {environmentName
            ? `${environmentName} will be read with the collection.`
            : "Choose it before the collection: it says which variables are secrets, and gives their values to the rest."}
        </span>
      </label>

      <label className="flex cursor-pointer flex-col items-center gap-2 rounded-xl border border-dashed px-6 py-12 text-center text-sm has-[:focus-visible]:ring-2 has-[:focus-visible]:ring-ring">
        <FileUp aria-hidden className="size-6 text-muted-foreground" />
        <span className="font-medium">{pending ? "Reading the files…" : "Choose a Postman collection"}</span>
        <span className="text-xs text-muted-foreground">Collection v2.0 or v2.1, JSON, up to 5 MB</span>
        <input
          type="file"
          accept=".json,application/json"
          disabled={pending}
          className="sr-only"
          onChange={(event) => {
            const file = event.target.files?.[0];
            if (file) onCollection(file);
            // Choosing the same file again has to fire a change.
            event.target.value = "";
          }}
        />
      </label>

      {error && (
        <p role="alert" className="rounded-lg border bg-card px-3 py-2 text-sm text-muted-foreground">
          {error}
        </p>
      )}
    </div>
  );
}

// ImportDone is what is left once the import has run: where the scenario and the
// endpoints went, the report, and the secrets to create.
function ImportDone({
  projectId,
  done,
  environments,
  canManage,
}: {
  projectId: string;
  done: Done;
  environments: Target[];
  canManage: boolean;
}) {
  return (
    <div className="flex flex-col gap-6">
      <section className="flex flex-col gap-2 rounded-xl border p-4 text-sm">
        <h2 className="font-medium">Imported</h2>
        {done.scenarioId && <p className="text-muted-foreground">The scenario was created.</p>}
        {done.catalogCount !== undefined && (
          <p className="text-muted-foreground">
            <span className="num">{done.catalogCount}</span> endpoints are in the project&apos;s catalog.
          </p>
        )}
        <div className="flex flex-wrap gap-2">
          {done.scenarioId && (
            <Link href={`/projects/${projectId}/scenarios/${done.scenarioId}`} className={buttonVariants()}>
              Open the scenario
            </Link>
          )}
          {done.catalogCount !== undefined && (
            <Link href={`/projects/${projectId}/endpoints`} className={buttonVariants({ variant: "outline" })}>
              See the endpoints
            </Link>
          )}
        </div>
      </section>

      {done.secrets.length > 0 && (
        <SecretsPanel projectId={projectId} secrets={done.secrets} environments={environments} canManage={canManage} />
      )}

      <ImportReport findings={done.findings} />
    </div>
  );
}
