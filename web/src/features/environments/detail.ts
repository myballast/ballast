import { getAccess, getProject } from "@/features/projects/api";
import type { Project } from "@/features/projects/types";
import { can, type Access } from "@/lib/permissions";

import { getChallenge, getEnvironment, getSecrets } from "./api";
import type { Environment, Secret, VerificationChallenge } from "./types";

export type EnvironmentPage = {
  project: Project;
  access: Access;
  environment: Environment;
  // The token and the secrets' keys take manage_environments. They are not
  // asked for without it, rather than asked for and refused.
  challenge: VerificationChallenge | null;
  secrets: Secret[] | null;
};

// loadEnvironmentPage reads everything the environment page shows, at once.
export async function loadEnvironmentPage(projectId: string, environmentId: string): Promise<EnvironmentPage> {
  const [project, access, environment] = await Promise.all([
    getProject(projectId),
    getAccess(projectId),
    getEnvironment(projectId, environmentId),
  ]);

  const manage = can(access, "manage_environments");
  const [challenge, secrets] = manage
    ? await Promise.all([getChallenge(projectId, environmentId), getSecrets(projectId, environmentId)])
    : [null, null];

  return { project, access, environment, challenge, secrets };
}
