// Pure text helpers, kept free of Obsidian imports so they're unit-testable.

/** GitHub-style heading slug: lowercase, punctuation stripped, spaces → hyphens. */
export function headingSlug(text: string): string {
	return text
		.toLowerCase()
		.trim()
		.replace(/[^\p{L}\p{N}\s-]/gu, "")
		.replace(/\s+/g, "-");
}

/** Deduplicates slugs within a document: repeat occurrences get -1, -2, … */
export function dedupeSlug(slug: string, used: Map<string, number>): string {
	if (!slug) return slug;
	const n = used.get(slug) ?? 0;
	used.set(slug, n + 1);
	return n === 0 ? slug : `${slug}-${n}`;
}

/** Clamps a string to max characters, ending on an ellipsis when cut. */
export function clamp(s: string, max = 200): string {
	return s.length <= max ? s : s.slice(0, max - 1).trimEnd() + "…";
}

/** Normalizes a frontmatter class list (string or YAML list) to safe CSS
 * class tokens, dropping anything that isn't a plain identifier. */
export function classList(v: unknown): string {
	const parts = Array.isArray(v) ? v.map(String) : typeof v === "string" ? v.split(/\s+/) : [];
	return parts.filter((c) => /^[A-Za-z0-9_-]+$/.test(c)).join(" ");
}
