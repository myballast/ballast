import { cache } from "react";
import { notFound } from "next/navigation";

import { ApiError, apiGet } from "@/lib/api-client";

import type { Environment, Secret, VerificationChallenge } from "./types";

function base(projectId: string, environmentId?: string): string {
  const project = `/projects/${encodeURIComponent(projectId)}/environments`;
  return environmentId ? `${project}/${encodeURIComponent(environmentId)}` : project;
}

// Cached for the request, like the scenarios: the shell and the page share it.
export const getEnvironments = cache(async (projectId: string): Promise<Environment[]> => {
  const { environments } = await apiGet<{ environments: Environment[] }>(base(projectId));
  return environments;
});

// getEnvironment reads one environment. No such environment, or one of another
// project, is the same 404 page: the API says the same for both.
export async function getEnvironment(projectId: string, environmentId: string): Promise<Environment> {
  try {
    return await apiGet<Environment>(base(projectId, environmentId));
  } catch (error) {
    if (error instanceof ApiError && (error.status === 404 || error.status === 400)) {
      notFound();
    }
    throw error;
  }
}

// getChallenge is the token and the URL the target has to serve it at. It
// takes manage_environments; callers only ask when the caller holds it.
export async function getChallenge(projectId: string, environmentId: string): Promise<VerificationChallenge> {
  return apiGet<VerificationChallenge>(`${base(projectId, environmentId)}/verification`);
}

// getSecrets lists the keys of the environment's secrets — never a value. It
// takes manage_environments as well.
export async function getSecrets(projectId: string, environmentId: string): Promise<Secret[]> {
  const { secrets } = await apiGet<{ secrets: Secret[] }>(`${base(projectId, environmentId)}/secrets`);
  return secrets;
}
