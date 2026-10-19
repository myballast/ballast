import type { Project } from "../types";

const field =
  "h-9 rounded-md border bg-background px-3 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring";

// DetailsFields are the optional things a person can say about a project: where
// it runs, who answers for it, what it is made of. Shared by the form that
// creates a project and the one that edits it, so the two cannot disagree about
// what a field is called or how it is read.
//
// All of them may be left empty. A project that says nothing is shown without
// them, and an empty field in the edit form clears what was there.
export function DetailsFields({ project }: { project?: Pick<Project, "infra" | "region" | "owner" | "stack"> }) {
  return (
    <>
      <div className="grid gap-4 sm:grid-cols-2">
        <label className="flex flex-col gap-1.5 text-sm">
          Infrastructure <span className="text-xs text-muted-foreground">optional</span>
          <input
            name="infra"
            maxLength={40}
            defaultValue={project?.infra}
            placeholder="k8s, railway, vm"
            className={field}
          />
        </label>
        <label className="flex flex-col gap-1.5 text-sm">
          Region <span className="text-xs text-muted-foreground">optional</span>
          <input
            name="region"
            maxLength={40}
            defaultValue={project?.region}
            placeholder="eu-west-1"
            className={`data ${field}`}
          />
        </label>
      </div>

      <label className="flex flex-col gap-1.5 text-sm">
        Owner <span className="text-xs text-muted-foreground">optional</span>
        <input
          name="owner"
          maxLength={80}
          defaultValue={project?.owner}
          placeholder="A person or a team"
          className={field}
        />
        <span className="text-xs text-muted-foreground">Text only: naming somebody here gives them no access.</span>
      </label>

      <label className="flex flex-col gap-1.5 text-sm">
        Stack <span className="text-xs text-muted-foreground">optional</span>
        <input
          name="stack"
          defaultValue={project?.stack.join(", ")}
          placeholder="go, pg, k6"
          className={`data ${field}`}
        />
        <span className="text-xs text-muted-foreground">
          Short labels separated by commas: up to 12, lowercase letters, digits and . _ + -
        </span>
      </label>
    </>
  );
}
