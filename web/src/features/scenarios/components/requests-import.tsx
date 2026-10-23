"use client";

import { FileUp, Info, Search } from "lucide-react";
import Link from "next/link";
import { useMemo, useState, useTransition } from "react";

import { Button, buttonVariants } from "@/components/ui/button";
import { importRequestsToCatalog } from "@/features/endpoints/actions";

import {
  chosenPositions,
  firstChoice,
  groupByFolder,
  limitFor,
  toggleGroup,
  type RequestGroup,
} from "../collection-selection";
import { defaultHost, defaultKinds, visibleRequests } from "../har-filter";
import { importRequests, previewRequests } from "../import-actions";
import { maxCurlBytes, maxHARBytes, maxSpecBytes } from "../import-selection";
import { requestsForm } from "../requests-body";
import type { CollectionPreview, ImportFinding, RequestKind, RequestsFormat } from "../types";
import { HARFilters } from "./har-filters";
import { ImportDestination } from "./import-destination";
import { ImportDone, type Done } from "./import-done";
import { CollectionPicker, CurlPicker, HARPicker } from "./import-pickers";
import { RequestList } from "./request-list";

const field =
  "h-9 rounded-md border bg-background px-3 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring";

type Target = { id: string; name: string };

// The text of the files, kept as it was read: the preview stores nothing on the
// server, so the import sends it again.
type Loaded = { text: string; environment: string; preview: CollectionPreview };

// What each format calls things on screen.
const wording: Record<RequestsFormat, { untitled: string; files: string; flat: string }> = {
  postman: { untitled: "Untitled collection", files: "these files", flat: "No folder" },
  har: { untitled: "HAR file", files: "this recording", flat: "Requests" },
  curl: { untitled: "cURL commands", files: "these commands", flat: "Commands" },
};

function Notice({ children }: { children: React.ReactNode }) {
  return (
    <p role="status" className="flex items-start gap-2 rounded-lg border bg-card px-3 py-2 text-xs text-muted-foreground">
      <Info aria-hidden className="mt-0.5 size-3.5 shrink-0" />
      <span>{children}</span>
    </p>
  );
}

// RequestsImport takes requests from a Postman collection, a HAR file or pasted
// cURL commands, in two steps: give the file, then tick the requests. All three
// are read into the same list, so everything after the first step is the same:
// what could not be carried over is said request by request, and no value of the
// file reaches the scenario.
export function RequestsImport({
  projectId,
  format,
  environments,
  canManageEnvironments,
}: {
  projectId: string;
  format: RequestsFormat;
  environments: Target[];
  canManageEnvironments: boolean;
}) {
  const words = wording[format];

  const [environmentFile, setEnvironmentFile] = useState<{ name: string; text: string } | null>(null);
  const [loaded, setLoaded] = useState<Loaded | null>(null);
  const [host, setHost] = useState("");
  const [kinds, setKinds] = useState<ReadonlySet<RequestKind>>(new Set());
  const [chosen, setChosen] = useState<ReadonlySet<number>>(new Set());
  const [search, setSearch] = useState("");
  const [name, setName] = useState("");
  const [makeScenario, setMakeScenario] = useState(true);
  const [addToCatalog, setAddToCatalog] = useState(false);
  const [source, setSource] = useState("");
  const [error, setError] = useState("");
  const [done, setDone] = useState<Done | null>(null);
  const [pending, startTransition] = useTransition();

  // A HAR file lists every host and kind of request it saw; the filters narrow
  // it. The other formats show what they hold.
  const shown = useMemo(() => {
    if (!loaded) return [];
    return format === "har" ? visibleRequests(loaded.preview.requests, host, kinds) : loaded.preview.requests;
  }, [loaded, format, host, kinds]);
  const groups = useMemo(() => groupByFolder(shown, search, words.flat), [shown, search, words.flat]);

  function refuseSize(label: string, size: number, limit: number): boolean {
    if (size <= limit) return false;
    setError(`${label} is larger than ${limit / (1024 * 1024)} MB, the most Ballast reads.`);
    return true;
  }

  async function chooseEnvironment(file: File) {
    setError("");
    if (refuseSize(file.name, file.size, maxSpecBytes)) return;
    setEnvironmentFile({ name: file.name, text: await file.text() });
  }

  function read(text: string, title: string) {
    startTransition(async () => {
      const environment = environmentFile?.text ?? "";
      const result = await previewRequests(requestsForm({ projectId, format, text, environment }));

      if ("error" in result) {
        setError(result.error);
        return;
      }

      const { requests } = result.preview;
      const firstHost = format === "har" ? defaultHost(requests) : "";
      const firstKinds = format === "har" ? defaultKinds(requests, firstHost) : new Set<RequestKind>();
      const named = (result.preview.name || title).slice(0, 120);

      setLoaded({ text, environment, preview: result.preview });
      setHost(firstHost);
      setKinds(firstKinds);
      setChosen(
        firstChoice(
          format === "har" ? visibleRequests(requests, firstHost, firstKinds) : requests,
          limitFor(true, result.preview.variable_nodes),
        ),
      );
      setSearch("");
      setName(named);
      setSource(named);
    });
  }

  function readFile(file: File) {
    setError("");
    const limit = format === "har" ? maxHARBytes : maxSpecBytes;
    if (refuseSize(file.name, file.size, limit)) return;

    startTransition(async () => read(await file.text(), file.name.replace(/\.[^.]+$/, "")));
  }

  function readCommands(text: string) {
    setError("");
    if (new Blob([text]).size > maxCurlBytes) return;
    read(text, words.untitled);
  }

  if (done) {
    return <ImportDone projectId={projectId} done={done} environments={environments} canManage={canManageEnvironments} />;
  }

  if (loaded === null) {
    if (format === "curl") return <CurlPicker pending={pending} error={error} onCommands={readCommands} />;
    if (format === "har") return <HARPicker pending={pending} error={error} onFile={readFile} />;
    return (
      <CollectionPicker
        pending={pending}
        error={error}
        environmentName={environmentFile?.name ?? ""}
        onCollection={readFile}
        onEnvironment={chooseEnvironment}
      />
    );
  }

  const { text, environment, preview } = loaded;
  const limit = limitFor(makeScenario, preview.variable_nodes);
  const full = chosen.size >= limit;

  function changeFilters(nextHost: string, nextKinds: ReadonlySet<RequestKind>) {
    setError("");
    setHost(nextHost);
    setKinds(nextKinds);
    // What is ticked belongs to what is shown: a new filter starts a new choice.
    setChosen(firstChoice(visibleRequests(preview.requests, nextHost, nextKinds), limit));
  }

  function toggleKind(kind: RequestKind) {
    const next = new Set(kinds);
    if (next.has(kind)) next.delete(kind);
    else next.add(kind);
    changeFilters(host, next);
  }

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
        const added = await importRequestsToCatalog(
          requestsForm({ projectId, format, text, environment, name: source.trim(), requests: positions }),
        );
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

      const created = await importRequests(
        requestsForm({ projectId, format, text, environment, name: name.trim(), requests: positions }),
      );
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
          <span className="font-medium">{preview.name || words.untitled}</span>{" "}
          <span className="num text-muted-foreground">
            · {preview.requests.length} {preview.requests.length === 1 ? "request" : "requests"}
          </span>
        </p>
        <Button type="button" variant="ghost" onClick={() => setLoaded(null)}>
          {format === "curl" ? "Paste other commands" : "Choose another file"}
        </Button>
      </section>

      <div className="flex flex-col gap-2">
        {preview.findings.map((finding) => (
          <Notice key={finding.message}>{finding.message}</Notice>
        ))}
        {preview.secrets.length > 0 && (
          <Notice>
            <span className="num">{preview.secrets.length}</span>{" "}
            {preview.secrets.length === 1 ? "secret was" : "secrets were"} found in {words.files}. None of{" "}
            {preview.secrets.length === 1 ? "its value is" : "their values are"} copied into the scenario: it reads{" "}
            {preview.secrets.length === 1 ? "it" : "each"} from the environment, and you create{" "}
            {preview.secrets.length === 1 ? "it" : "them"} after the import.
          </Notice>
        )}
      </div>

      {format === "har" && (
        <HARFilters
          requests={preview.requests}
          host={host}
          kinds={kinds}
          onHost={(next) => changeFilters(next, defaultKinds(preview.requests, next))}
          onKind={toggleKind}
        />
      )}

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
