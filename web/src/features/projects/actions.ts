"use server";

import { refresh, revalidatePath } from "next/cache";
import { cookies } from "next/headers";
import { redirect } from "next/navigation";

import { ApiError, apiDelete, apiPatch, apiPost, apiPut } from "@/lib/api-client";

import { isSortKey, isViewMode, parseStack, type SortKey, type ViewMode } from "./arrangement";
import { sortCookie, viewCookie } from "./preferences";

export type CreateProjectState = {
  error?: string;
  created?: boolean;
};

// createProject is the project list's "New project" form. Every check that
// matters is the API's: this turns its refusals into sentences.
export async function createProject(
  _previous: CreateProjectState,
  form: FormData,
): Promise<CreateProjectState> {
  const name = String(form.get("name") ?? "").trim();
  const slug = String(form.get("slug") ?? "").trim();
  if (!name || !slug) {
    return { error: "A project needs a name and a slug." };
  }

  // Optional, and sent only when written: an empty field says nothing.
  const details = detailsOf(form);

  try {
    await apiPost("/projects", { name, slug, ...details });
  } catch (error) {
    if (!(error instanceof ApiError)) throw error;

    switch (error.status) {
      case 401:
        redirect("/login?next=%2Fprojects");
      case 403:
        return {
          error:
            "Creating a project takes the admin role on a project of this organization. Ask one of its admins.",
        };
      case 409:
        return { error: `Another project already uses the slug “${slug}”. Pick another one.` };
      case 400:
        // The API's 400 messages are written for whoever sent the request.
        return { error: error.message };
    }
    throw error;
  }

  refresh();
  return { created: true };
}

// detailsOf reads the optional details of a project from a form. A field that is
// left empty is sent empty, which is how a detail is cleared; the API decides
// what a label may hold, and its refusal names it.
function detailsOf(form: FormData) {
  const text = (name: string) => String(form.get(name) ?? "").trim();
  return { infra: text("infra"), region: text("region"), owner: text("owner"), stack: parseStack(text("stack")) };
}

export type DetailsState = { error?: string; saved?: boolean };

// updateProjectDetails is "Edit details" on the project's page.
export async function updateProjectDetails(
  projectId: string,
  _previous: DetailsState,
  form: FormData,
): Promise<DetailsState> {
  try {
    await apiPatch(`/projects/${encodeURIComponent(projectId)}`, detailsOf(form));
  } catch (error) {
    if (!(error instanceof ApiError)) throw error;

    switch (error.status) {
      case 401:
        redirect(`/login?next=${encodeURIComponent(`/projects/${projectId}`)}`);
      case 403:
        return { error: "Editing a project takes the admin role on it." };
      case 404:
        return { error: "This project no longer exists. Reload the page." };
      case 400:
        return { error: error.message };
    }
    throw error;
  }

  // The shell around the project, and the list it appears in, were drawn with
  // the old details.
  revalidatePath("/projects", "layout");
  return { saved: true };
}

// setFavorite stars or unstars a project for the person asking, and for nobody
// else. It answers nothing: the page is drawn again with the new order.
export async function setFavorite(projectId: string, on: boolean): Promise<void> {
  const path = `/projects/${encodeURIComponent(projectId)}/favorite`;

  try {
    await (on ? apiPut(path, {}) : apiDelete(path));
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) redirect("/login?next=%2Fprojects");
    // 403 and 404 mean the project is not theirs any more: the page drawn again
    // will not list it.
    if (!(error instanceof ApiError) || (error.status !== 403 && error.status !== 404)) throw error;
  }

  refresh();
}

// savePreferences remembers how this browser likes the project list: the view and
// the order. A cookie rather than a setting kept on the account: it is a matter
// of how a screen looks, it is read by the page on the server so that nothing
// flickers, and it needs no endpoint. A value that is not one of the known ones
// is not stored.
export async function savePreferences(preferences: { view?: ViewMode; sort?: SortKey }): Promise<void> {
  const jar = await cookies();
  const options = { path: "/", maxAge: 60 * 60 * 24 * 365, sameSite: "lax" as const };

  if (isViewMode(preferences.view)) jar.set(viewCookie, preferences.view, options);
  if (isSortKey(preferences.sort)) jar.set(sortCookie, preferences.sort, options);

  refresh();
}
