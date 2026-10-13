"use server";

import { refresh } from "next/cache";
import { redirect } from "next/navigation";

import { ApiError, apiPost } from "@/lib/api-client";

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

  try {
    await apiPost("/projects", { name, slug });
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
