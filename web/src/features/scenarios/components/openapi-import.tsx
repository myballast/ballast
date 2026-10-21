"use client";

import { FileUp, Info, Search } from "lucide-react";
import Link from "next/link";
import { useState, useTransition } from "react";

import { Button, buttonVariants } from "@/components/ui/button";

import { importOpenAPI, previewOpenAPI } from "../import-actions";
import {
  chosenInOrder,
  countWithPathParams,
  groupByTag,
  maxChosenEndpoints,
  maxSpecBytes,
  toggleGroup,
  type EndpointGroup,
} from "../import-selection";
import type { OpenAPIPreview } from "../types";
import { EndpointList } from "./endpoint-list";

const field =
  "h-9 rounded-md border bg-background px-3 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring";

// What was read from the file. The text is kept: the preview stores nothing on
// the server, so the import sends the file again.
type Loaded = { spec: string; preview: OpenAPIPreview };

function Notice({ children }: { children: React.ReactNode }) {
  return (
    <p role="status" className="flex items-start gap-2 rounded-lg border bg-card px-3 py-2 text-xs text-muted-foreground">
      <Info aria-hidden className="mt-0.5 size-3.5 shrink-0" />
      <span>{children}</span>
    </p>
  );
}

// OpenAPIImport takes a specification in two steps: pick the file, then tick the
// endpoints. A client component because the file is read in the browser and the
// choice lives here until it is sent.
export function OpenAPIImport({ projectId }: { projectId: string }) {
  const [loaded, setLoaded] = useState<Loaded | null>(null);
  const [chosen, setChosen] = useState<ReadonlySet<string>>(new Set());
  const [search, setSearch] = useState("");
  const [name, setName] = useState("");
  const [error, setError] = useState("");
  const [pending, startTransition] = useTransition();

  function readFile(file: File) {
    setError("");

    if (file.size > maxSpecBytes) {
      setError("That file is larger than 5 MB, the most Ballast reads.");
      return;
    }

    startTransition(async () => {
      const spec = await file.text();
      const result = await previewOpenAPI(projectId, spec);

      if ("error" in result) {
        setError(result.error);
        return;
      }

      setLoaded({ spec, preview: result.preview });
      setChosen(new Set());
      setSearch("");
      setName((result.preview.title || file.name.replace(/\.[^.]+$/, "")).slice(0, 120));
    });
  }

  if (loaded === null) {
    return <FilePicker pending={pending} error={error} onFile={readFile} />;
  }

  const { spec, preview } = loaded;
  const groups = groupByTag(preview.endpoints, search);
  const full = chosen.size >= maxChosenEndpoints;
  const withPathParams = countWithPathParams(preview.endpoints, chosen);

  function toggle(key: string) {
    setError("");
    const next = new Set(chosen);
    if (next.has(key)) next.delete(key);
    else next.add(key);
    setChosen(next);
  }

  function toggleWholeGroup(group: EndpointGroup) {
    const next = toggleGroup(chosen, group);
    if (next.size > maxChosenEndpoints) {
      setError(`A scenario holds at most ${maxChosenEndpoints} requests.`);
      return;
    }
    setError("");
    setChosen(next);
  }

  function importChosen() {
    setError("");

    startTransition(async () => {
      const result = await importOpenAPI(projectId, name.trim(), spec, chosenInOrder(preview.endpoints, chosen));
      setError(result.error);
    });
  }

  return (
    <div className="flex flex-col gap-6">
      <section className="flex flex-wrap items-center justify-between gap-3">
        <p className="text-sm">
          <span className="font-medium">{preview.title || "Untitled specification"}</span>{" "}
          <span className="num text-muted-foreground">
            · {preview.endpoints.length} {preview.endpoints.length === 1 ? "endpoint" : "endpoints"}
          </span>
        </p>
        <Button type="button" variant="ghost" onClick={() => setLoaded(null)}>
          Choose another file
        </Button>
      </section>

      <div className="flex flex-col gap-2">
        {preview.notes.map((note) => (
          <Notice key={note}>{note}</Notice>
        ))}
        {preview.schemes.length > 0 && (
          <Notice>
            This API declares {preview.schemes.map((s) => `${s.name} (${s.detail || s.type})`).join(", ")}. The requests
            are imported without any credential: Ballast never copies one into a scenario.
          </Notice>
        )}
        {withPathParams > 0 && (
          <Notice>
            <span className="num">{withPathParams}</span> of the chosen requests have a path parameter such as{" "}
            <code className="data text-foreground">{"{id}"}</code>. Each needs a value before the scenario runs.
          </Notice>
        )}
      </div>

      <label className="relative flex max-w-md items-center text-sm">
        <span className="sr-only">Search endpoints</span>
        <Search aria-hidden className="pointer-events-none absolute left-3 size-4 text-muted-foreground" />
        <input
          type="search"
          value={search}
          onChange={(event) => setSearch(event.target.value)}
          placeholder="Search by path, method, tag or summary"
          className={`${field} w-full pl-9`}
        />
      </label>

      <EndpointList groups={groups} chosen={chosen} full={full} onToggle={toggle} onToggleGroup={toggleWholeGroup} />

      <form
        className="flex flex-col gap-4 rounded-xl border bg-card p-4"
        onSubmit={(event) => {
          event.preventDefault();
          importChosen();
        }}
      >
        <label className="flex max-w-xl flex-col gap-1.5 text-sm">
          Scenario name
          <input
            required
            maxLength={120}
            value={name}
            onChange={(event) => setName(event.target.value)}
            className={field}
          />
        </label>

        {error && (
          <p role="alert" className="rounded-lg border bg-background px-3 py-2 text-sm text-muted-foreground">
            {error}
          </p>
        )}

        <div className="flex flex-wrap items-center gap-2">
          <Button type="submit" disabled={pending || chosen.size === 0 || name.trim() === ""}>
            <FileUp aria-hidden />
            {pending ? "Importing…" : `Import ${chosen.size} ${chosen.size === 1 ? "endpoint" : "endpoints"}`}
          </Button>
          <Link href={`/projects/${projectId}/scenarios`} className={buttonVariants({ variant: "ghost" })}>
            Cancel
          </Link>
        </div>
      </form>
    </div>
  );
}

// FilePicker is the first step: a file input, and whatever went wrong with the
// last file.
function FilePicker({
  pending,
  error,
  onFile,
}: {
  pending: boolean;
  error: string;
  onFile: (file: File) => void;
}) {
  return (
    <div className="flex max-w-xl flex-col gap-3">
      <label className="flex cursor-pointer flex-col items-center gap-2 rounded-xl border border-dashed px-6 py-12 text-center text-sm has-[:focus-visible]:ring-2 has-[:focus-visible]:ring-ring">
        <FileUp aria-hidden className="size-6 text-muted-foreground" />
        <span className="font-medium">{pending ? "Reading the file…" : "Choose a specification"}</span>
        <span className="text-xs text-muted-foreground">JSON or YAML, up to 5 MB</span>
        <input
          type="file"
          accept=".json,.yaml,.yml,application/json,text/yaml,application/yaml"
          disabled={pending}
          className="sr-only"
          onChange={(event) => {
            const file = event.target.files?.[0];
            if (file) onFile(file);
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
