import { getEnvironments } from "@/features/environments/api";
import { getLastRun } from "@/features/runs/api";
import { cache } from "react";
import { notFound } from "next/navigation";

import { ApiError, apiGet } from "@/lib/api-client";
import type { Access } from "@/lib/permissions";

import type { Project, ProjectOverview } from "./types";

// getProjects is the caller's projects: the API returns those they are a
// member of, and no others.
export async function getProjects(): Promise<Project[]> {
  const { projects } = await apiGet<{ projects: Project[] }>("/projects");
  return projects;
}

// getProjectOverviews reads each project's environments and last run, every
// project at once rather than one after the other.
export async function getProjectOverviews(): Promise<ProjectOverview[]> {
  const projects = await getProjects();
  return Promise.all(projects.map(overview));
}

async function overview(project: Project): Promise<ProjectOverview> {
  try {
    const [environments, lastRun] = await Promise.all([
      getEnvironments(project.id),
      getLastRun(project.id),
    ]);
    return { project, access: "full", environments, lastRun };
  } catch (error) {
    if (error instanceof ApiError && error.status === 403) {
      return { project, access: "restricted" };
    }
    throw error;
  }
}

// findProject reads one project, or null when there is none for the caller:
// not a member (403), or no such project, answer the same.
//
// Cached for the request: the shell around a page and the page itself both ask,
// and one answer serves them both.
export const findProject = cache(async (projectId: string): Promise<Project | null> => {
  try {
    return await apiGet<Project>(`/projects/${encodeURIComponent(projectId)}`);
  } catch (error) {
    if (error instanceof ApiError && (error.status === 404 || error.status === 403 || error.status === 400)) {
      return null;
    }
    throw error;
  }
});

// getProject reads one project. Not a member, or no such project, is the
// same 404 page: the API says the same for both.
export async function getProject(projectId: string): Promise<Project> {
  return (await findProject(projectId)) ?? notFound();
}

// findAccess is what the caller holds in the project, or null when they are not
// a member of it. The role comes from the membership, resolved by the API on
// every request.
export const findAccess = cache(async (projectId: string): Promise<Access | null> => {
  try {
    return await apiGet<Access>(`/projects/${encodeURIComponent(projectId)}/permissions`);
  } catch (error) {
    if (error instanceof ApiError && (error.status === 404 || error.status === 403 || error.status === 400)) {
      return null;
    }
    throw error;
  }
});

// getAccess is what the caller holds in the project, to hide what they
// cannot use. Not being a member is the same 404 page as no such project.
export async function getAccess(projectId: string): Promise<Access> {
  return (await findAccess(projectId)) ?? notFound();
}
