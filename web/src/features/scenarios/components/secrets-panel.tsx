"use client";

import { KeyRound } from "lucide-react";
import { useState, useTransition } from "react";

import { Button } from "@/components/ui/button";
import { setSecretValues } from "@/features/environments/actions";

import type { DetectedSecret } from "../types";

const field =
  "h-9 rounded-md border bg-background px-3 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring";

type Target = { id: string; name: string };

// SecretsPanel is where the person types the secrets an import found. The import
// read none of their values: the scenario reads each from its environment, under
// the key listed here, and this form is where those values are put there.
//
// Nothing is filled in. The values are typed again, which is what keeps them out
// of the file's journey through Ballast, and an empty field is skipped so that
// somebody can do some now and the rest later.
export function SecretsPanel({
  projectId,
  secrets,
  environments,
  canManage,
}: {
  projectId: string;
  secrets: DetectedSecret[];
  environments: Target[];
  canManage: boolean;
}) {
  const [environmentId, setEnvironmentId] = useState(environments[0]?.id ?? "");
  const [values, setValues] = useState<Record<string, string>>({});
  const [saved, setSaved] = useState<string[]>([]);
  const [failed, setFailed] = useState<{ key: string; error: string }[]>([]);
  const [pending, startTransition] = useTransition();

  const keys = secrets.map((secret) => secret.key);

  if (!canManage || environments.length === 0) {
    return (
      <section className="flex flex-col gap-2 rounded-xl border p-4 text-sm">
        <h2 className="font-medium">Secrets to create</h2>
        <p className="text-muted-foreground">
          The scenario reads these from an environment, and sends an empty value for each until they exist.{" "}
          {canManage
            ? "This project has no environment yet: create one first."
            : "Creating them takes the maintainer role: ask somebody who has it."}
        </p>
        <p className="data break-all">{keys.join(", ")}</p>
      </section>
    );
  }

  function save() {
    startTransition(async () => {
      const entries = secrets.map((secret) => ({ key: secret.key, value: values[secret.key] ?? "" }));
      const result = await setSecretValues(projectId, environmentId, entries);

      setSaved((before) => [...before, ...result.saved]);
      setFailed(result.failed);
      // What was stored is gone from the screen: it is not shown again.
      setValues((before) => {
        const next = { ...before };
        for (const key of result.saved) delete next[key];
        return next;
      });
    });
  }

  return (
    <section className="flex flex-col gap-3 rounded-xl border p-4 text-sm">
      <h2 className="font-medium">Secrets to create</h2>
      <p className="max-w-xl text-muted-foreground">
        The scenario reads these from an environment. Ballast did not take their values from your files: type each one
        here, and it goes straight to the environment. Leave one empty to do it later.
      </p>

      <label className="flex max-w-xs flex-col gap-1.5">
        Environment
        <select value={environmentId} onChange={(event) => setEnvironmentId(event.target.value)} className={field}>
          {environments.map((environment) => (
            <option key={environment.id} value={environment.id}>
              {environment.name}
            </option>
          ))}
        </select>
      </label>

      <ul className="flex flex-col gap-2">
        {secrets.map((secret) => (
          <li key={secret.key} className="flex flex-wrap items-center gap-3">
            <label className="flex flex-wrap items-center gap-3">
              <span className="data w-56 shrink-0 break-all">{secret.key}</span>
              <input
                type="password"
                autoComplete="off"
                value={values[secret.key] ?? ""}
                onChange={(event) => setValues((before) => ({ ...before, [secret.key]: event.target.value }))}
                aria-label={`Value of ${secret.key}`}
                disabled={saved.includes(secret.key)}
                className={`${field} w-72`}
              />
            </label>
            {saved.includes(secret.key) && <span className="text-xs text-muted-foreground">saved</span>}
          </li>
        ))}
      </ul>

      {failed.map((failure) => (
        <p key={failure.key} role="alert" className="text-xs text-muted-foreground">
          {failure.key}: {failure.error}
        </p>
      ))}

      <div>
        <Button type="button" onClick={save} disabled={pending || environmentId === ""}>
          <KeyRound aria-hidden />
          {pending ? "Saving…" : "Save the secrets"}
        </Button>
      </div>
    </section>
  );
}
