"use client";

import { Pencil, X } from "lucide-react";
import { useActionState, useRef } from "react";

import { Button } from "@/components/ui/button";

import { updateProjectDetails, type DetailsState } from "../actions";
import type { Project } from "../types";
import { DetailsFields } from "./details-fields";

// EditDetails is "Edit details" on a project's page, for whoever administers it.
// A dialog, so that the form is in front of the page it changes and a refusal is
// shown where the person is looking.
export function EditDetails({ project }: { project: Project }) {
  const dialog = useRef<HTMLDialogElement>(null);

  return (
    <>
      <Button variant="outline" onClick={() => dialog.current?.showModal()}>
        <Pencil aria-hidden />
        Edit details
      </Button>

      <dialog
        ref={dialog}
        aria-labelledby="edit-details-title"
        className="m-auto w-[min(30rem,calc(100vw-2rem))] rounded-xl border bg-card p-0 text-left text-foreground backdrop:bg-background/70"
      >
        <DetailsForm project={project} onDone={() => dialog.current?.close()} />
      </dialog>
    </>
  );
}

function DetailsForm({ project, onDone }: { project: Project; onDone: () => void }) {
  const [state, action, pending] = useActionState<DetailsState, FormData>(
    async (previous, form) => {
      const next = await updateProjectDetails(project.id, previous, form);
      if (next.saved) onDone();
      return next;
    },
    {},
  );

  return (
    <form action={action} className="flex flex-col gap-4 p-5">
      <div className="flex items-start justify-between gap-3">
        <h2 id="edit-details-title" className="text-lg font-normal tracking-tight">
          Details of {project.name}
        </h2>
        <button
          type="button"
          onClick={onDone}
          className="grid size-7 place-items-center rounded-md text-muted-foreground hover:bg-muted hover:text-foreground"
        >
          <X aria-hidden className="size-4" />
          <span className="sr-only">Close</span>
        </button>
      </div>

      <DetailsFields project={project} />

      {state.error && (
        <p role="alert" className="text-sm text-muted-foreground">
          {state.error}
        </p>
      )}

      <div className="flex gap-2">
        <Button type="submit" disabled={pending}>
          {pending ? "Saving…" : "Save details"}
        </Button>
        <Button type="button" variant="ghost" onClick={onDone} disabled={pending}>
          Cancel
        </Button>
      </div>
    </form>
  );
}
