import type { Environment } from "@/features/environments/types";
import type { RunSummary } from "@/features/runs/types";

// Mirror of dto.Project in internal/adapter/http/dto.
export type Project = {
  id: string;
  name: string;
  slug: string;
  // Optional details a person wrote. Always present: empty strings and an empty
  // array when not said, so a card never has to guard against undefined.
  infra: string;
  region: string;
  owner: string;
  stack: string[];
  // Whether the signed-in person has starred it. Their own star only.
  favorite: boolean;
  created_at: string;
};

// ProjectOverview is a project as the project list shows it.
//
// Belonging to a project does not always mean seeing inside it: a membership
// can be given a narrower mask than its role. Such a project is still listed,
// with its details marked restricted rather than shown as empty, which would
// read as "no environment, never run" and be false.
export type ProjectOverview =
  | {
      project: Project;
      access: "full";
      environments: Environment[];
      // null when the project has never run.
      lastRun: RunSummary | null;
    }
  | { project: Project; access: "restricted" };
