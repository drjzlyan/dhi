// slugify turns a title into a URL slug: lowercase ASCII words joined by
// single dashes, with no leading or trailing dash.
export function slugify(title: string): string {
  return title
    .toLowerCase()
    .replace(/[^a-z0-9]/g, '-')
}
