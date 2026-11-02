"use client";

import { Plus, X } from "lucide-react";
import { useActionState, useEffect, useRef, useState } from "react";

import { Button } from "@/components/ui/button";

import { createProject, type CreateProjectState } from "../actions";
import { slugify } from "../slug";
import { DetailsFields } from "./details-fields";

// CreateProject is the "New project" button and its form, in a dialog: the form is
// in front of the page it adds to, a refusal is shown where the person is looking,
// and the list under it does not move. The form is only there while the dialog is
// open, so that each opening starts from an empty one.
export function CreateProject() {
  const dialog = useRef<HTMLDialogElement>(null);
  const [open, setOpen] = useState(false);

  useEffect(() => {
    const element = dialog.current;
    if (!element) return;
    if (open && !element.open) element.showModal();
    if (!open && element.open) element.close();
  }, [open]);

  return (
    <>
      <Button onClick={() => setOpen(true)}>
        <Plus aria-hidden />
        New project
      </Button>

      {/* Escape closes the dialog on its own; onClose keeps the state in step. */}
      <dialog
        ref={dialog}
        aria-labelledby="new-project-title"
        onClose={() => setOpen(false)}
        className="m-auto max-h-[calc(100svh-2rem)] w-[min(30rem,calc(100vw-2rem))] overflow-y-auto rounded-xl border bg-card p-0 text-left text-foreground backdrop:bg-background/70"
      >
        {open && <NewProjectForm onDone={() => setOpen(false)} />}
      </dialog>
    </>
  );
}

// NewProjectForm is a client component for one reason: the slug follows the name as
// it is typed, until the user edits the slug themselves.
function NewProjectForm({ onDone }: { onDone: () => void }) {
  const [name, setName] = useState("");
  const [slug, setSlug] = useState("");
  const [slugEdited, setSlugEdited] = useState(false);

  // A created project closes the dialog; the list itself has been refreshed by the
  // server action.
  const [state, action, pending] = useActionState<CreateProjectState, FormData>(async (previous, form) => {
    const next = await createProject(previous, form);
    if (next.created) onDone();
    return next;
  }, {});

  return (
    <form action={action} className="flex flex-col gap-4 p-5">
      <div className="flex items-start justify-between gap-3">
        <h2 id="new-project-title" className="text-lg font-normal tracking-tight">
          New project
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

      <label className="flex flex-col gap-1.5 text-sm">
        Name
        <input
          autoFocus
          name="name"
          required
          maxLength={80}
          value={name}
          onChange={(event) => {
            setName(event.target.value);
            if (!slugEdited) setSlug(slugify(event.target.value));
          }}
          className="h-9 rounded-md border bg-background px-3 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring"
        />
      </label>

      <label className="flex flex-col gap-1.5 text-sm">
        Slug
        <input
          name="slug"
          required
          maxLength={40}
          pattern="[a-z0-9]+(-[a-z0-9]+)*"
          title="Lowercase letters and digits, joined by single hyphens"
          value={slug}
          onChange={(event) => {
            setSlug(event.target.value);
            setSlugEdited(true);
          }}
          className="data h-9 rounded-md border bg-background px-3 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring"
        />
        <span className="text-xs text-muted-foreground">
          Appears in URLs, and cannot be changed once the project exists.
        </span>
      </label>

      <DetailsFields />

      {state.error && (
        <p role="alert" className="text-sm text-muted-foreground">
          {state.error}
        </p>
      )}

      <div className="flex gap-2">
        <Button type="submit" disabled={pending}>
          {pending ? "Creating…" : "Create project"}
        </Button>
        <Button type="button" variant="ghost" onClick={onDone} disabled={pending}>
          Cancel
        </Button>
      </div>
    </form>
  );
}
