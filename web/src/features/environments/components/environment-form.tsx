"use client";

import { TriangleAlert } from "lucide-react";
import Link from "next/link";
import { useActionState, useState } from "react";

import { buttonVariants, Button } from "@/components/ui/button";

import { createEnvironment, updateEnvironment, type FormState } from "../actions";
import type { Environment } from "../types";

const field =
  "h-9 rounded-md border bg-background px-3 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring";

// The origin is what the proof was served from: the API resets the
// verification when it changes, and only then.
function originOf(url: string): string | null {
  try {
    return new URL(url).origin;
  } catch {
    return null;
  }
}

function browserTimezone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone;
  } catch {
    return "UTC";
  }
}

// EnvironmentForm declares a target, or edits one. A client component because
// it says, before anything is sent, when an edit is about to throw away the
// verification.
export function EnvironmentForm({ projectId, environment }: { projectId: string; environment?: Environment }) {
  const submit = environment
    ? updateEnvironment.bind(null, projectId, environment.id)
    : createEnvironment.bind(null, projectId);
  const [state, action, pending] = useActionState<FormState, FormData>(submit, {});

  const [baseURL, setBaseURL] = useState(environment?.base_url ?? "");
  const [windowed, setWindowed] = useState(environment?.allowed_window !== undefined);

  const retargeted =
    environment !== undefined &&
    environment.verification_status !== "pending" &&
    originOf(baseURL) !== originOf(environment.base_url);

  const cancel = environment
    ? `/projects/${projectId}/environments/${environment.id}`
    : `/projects/${projectId}/environments`;

  return (
    <form action={action} className="flex max-w-xl flex-col gap-6">
      <label className="flex flex-col gap-1.5 text-sm">
        Name
        <input
          name="name"
          required
          maxLength={120}
          defaultValue={environment?.name}
          placeholder="staging"
          className={field}
        />
      </label>

      <label className="flex flex-col gap-1.5 text-sm">
        Base URL
        <input
          name="base_url"
          type="url"
          required
          value={baseURL}
          onChange={(event) => setBaseURL(event.target.value)}
          placeholder="https://staging.example.com"
          className={`data ${field}`}
        />
        <span className="text-xs text-muted-foreground">
          Load goes to this origin only, once you have proved you own it and an admin has approved it.
        </span>
      </label>

      {retargeted && (
        <p role="alert" className="flex items-start gap-2 rounded-lg border bg-card px-3 py-2 text-sm text-muted-foreground">
          <TriangleAlert aria-hidden className="mt-0.5 size-4 shrink-0" />
          <span>
            This changes the origin. The proof was served by the old one, so the environment goes back to not
            verified: it has to be verified and approved again before any run.
          </span>
        </p>
      )}

      <div className="grid gap-4 sm:grid-cols-2">
        <label className="flex flex-col gap-1.5 text-sm">
          Max virtual users
          <input
            name="max_vus"
            type="number"
            required
            min={1}
            step={1}
            defaultValue={environment?.max_vus ?? 10}
            className={`num ${field}`}
          />
        </label>
        <label className="flex flex-col gap-1.5 text-sm">
          Max duration (seconds)
          <input
            name="max_duration_s"
            type="number"
            required
            min={1}
            step={1}
            defaultValue={environment?.max_duration_s ?? 300}
            className={`num ${field}`}
          />
        </label>
        <span className="text-xs text-muted-foreground sm:col-span-2">
          Every run against this environment is clamped to these ceilings, whatever it asks for.
        </span>
      </div>

      <fieldset className="flex flex-col gap-3 text-sm">
        <label className="flex items-center gap-2">
          <input
            type="checkbox"
            name="window"
            checked={windowed}
            onChange={(event) => setWindowed(event.target.checked)}
          />
          Only take load during an allowed window
        </label>
        {windowed && (
          <div className="grid gap-4 sm:grid-cols-3">
            <label className="flex flex-col gap-1.5">
              From
              <input
                name="window_start"
                type="time"
                required
                defaultValue={environment?.allowed_window?.start ?? "22:00"}
                className={`num ${field}`}
              />
            </label>
            <label className="flex flex-col gap-1.5">
              To
              <input
                name="window_end"
                type="time"
                required
                defaultValue={environment?.allowed_window?.end ?? "06:00"}
                className={`num ${field}`}
              />
            </label>
            <label className="flex flex-col gap-1.5">
              Timezone
              <input
                name="window_timezone"
                required
                defaultValue={environment?.allowed_window?.timezone ?? browserTimezone()}
                placeholder="Europe/Paris"
                className={`data ${field}`}
              />
            </label>
            <span className="text-xs text-muted-foreground sm:col-span-3">
              A run has to fit entirely inside the window, or it is refused. A start after the end crosses midnight.
            </span>
          </div>
        )}
      </fieldset>

      {state.error && (
        <p role="alert" className="text-sm text-muted-foreground">
          {state.error}
        </p>
      )}

      <div className="flex gap-2">
        <Button type="submit" disabled={pending}>
          {pending ? "Saving…" : environment ? "Save changes" : "Declare environment"}
        </Button>
        <Link href={cancel} className={buttonVariants({ variant: "ghost" })}>
          Cancel
        </Link>
      </div>
    </form>
  );
}
