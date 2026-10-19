"use client";

import { Plus } from "lucide-react";
import { useActionState, useState } from "react";

import { Button } from "@/components/ui/button";

import { createProject, type CreateProjectState } from "../actions";
import { slugify } from "../slug";
import { DetailsFields } from "./details-fields";

// CreateProject is the "New project" button and its form. A client component
// because the slug follows the name as it is typed, until the user edits the
// slug themselves.
export function CreateProject() {
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const [slug, setSlug] = useState("");
  const [slugEdited, setSlugEdited] = useState(false);

  // A created project closes and resets the form; the list itself has been
  // refreshed by the server action.
  const [state, action, pending] = useActionState<CreateProjectState, FormData>(
    async (previous, form) => {
      const next = await createProject(previous, form);
      if (next.created) {
        setOpen(false);
        setName("");
        setSlug("");
        setSlugEdited(false);
      }
      return next;
    },
    {},
  );

  if (!open) {
    return (
      <Button onClick={() => setOpen(true)}>
        <Plus aria-hidden />
        New project
      </Button>
    );
  }

  return (
    <form
      action={action}
      aria-label="New project"
      className="flex w-full flex-col gap-4 rounded-xl border bg-card p-4 sm:max-w-md"
    >
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
        <Button type="button" variant="ghost" onClick={() => setOpen(false)} disabled={pending}>
          Cancel
        </Button>
      </div>
    </form>
  );
}
