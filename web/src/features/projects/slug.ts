// slugify proposes a slug from a project's name, in the shape the API
// accepts: lowercase letters and digits in groups joined by single hyphens,
// at most 40 characters. The user can still change it before creating.
export function slugify(name: string): string {
  return name
    .normalize("NFD")
    .replace(/[̀-ͯ]/g, "")
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 40)
    .replace(/-+$/, "");
}
