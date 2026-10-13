import { getEnvironments } from "@/features/environments/api";
import { getLastRun } from "@/features/runs/api";
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

// getProject reads one project. Not a member, or no such project, is the
// same 404 page: the API says the same for both.
export async function getProject(projectId: string): Promise<Project> {
  try {
    return await apiGet<Project>(`/projects/${encodeURIComponent(projectId)}`);
  } catch (error) {
    if (error instanceof ApiError && (error.status === 404 || error.status === 400)) {
      notFound();
    }
    throw error;
  }
}

// getAccess is what the caller holds in the project, to hide what they
// cannot use.
export async function getAccess(projectId: string): Promise<Access> {
  return apiGet<Access>(`/projects/${encodeURIComponent(projectId)}/permissions`);
}
