import type { RequestsFormat } from "./types";

// What the import screen sends to a server action. A string argument of a server
// action that is about a megabyte long is refused by the framework before the
// action runs, and a HAR file is up to 25 MB: so the text travels as the fields
// of a FormData, which carries any size up to the body limit.
export type RequestsForm = {
  projectId: string;
  format: RequestsFormat;
  // The file, or the pasted commands.
  text: string;
  // The Postman environment file; empty for the other formats.
  environment: string;
  // The scenario name, or the catalog source, depending on the action.
  name: string;
  requests: number[];
};

export function requestsForm(fields: Partial<RequestsForm> & Pick<RequestsForm, "projectId" | "format" | "text">): FormData {
  const form = new FormData();
  form.set("projectId", fields.projectId);
  form.set("format", fields.format);
  form.set("text", fields.text);
  form.set("environment", fields.environment ?? "");
  form.set("name", fields.name ?? "");
  form.set("requests", JSON.stringify(fields.requests ?? []));
  return form;
}

const formats: readonly string[] = ["postman", "har", "curl"];

function text(form: FormData, key: string): string {
  const value = form.get(key);
  return typeof value === "string" ? value : "";
}

// readRequestsForm reads what requestsForm wrote. The format becomes a segment of
// the API path, so anything that is not one of the three is refused here: null.
export function readRequestsForm(form: FormData): RequestsForm | null {
  const format = text(form, "format");
  if (!formats.includes(format)) return null;

  let requests: number[] = [];
  try {
    const parsed: unknown = JSON.parse(text(form, "requests") || "[]");
    if (Array.isArray(parsed)) requests = parsed.filter((position): position is number => Number.isInteger(position));
  } catch {
    return null;
  }

  return {
    projectId: text(form, "projectId"),
    format: format as RequestsFormat,
    text: text(form, "text"),
    environment: text(form, "environment"),
    name: text(form, "name"),
    requests,
  };
}

// filesOf names what each format sends to the API: a collection and its
// environment, a HAR file, or the pasted commands. The API reads each under its
// own limit.
export function filesOf(format: RequestsFormat, text: string, environment: string): Record<string, string> {
  switch (format) {
    case "har":
      return { har: text };
    case "curl":
      return { commands: text };
    default:
      return { collection: text, environment };
  }
}
