import { describe, expect, it } from "vitest";
import { clamp, classList, dedupeSlug, headingSlug } from "./text";

describe("headingSlug", () => {
	it("lowercases and hyphenates", () => {
		expect(headingSlug("My Heading")).toBe("my-heading");
		expect(headingSlug("  Trimmed  Spaces  ")).toBe("trimmed-spaces");
	});

	it("strips punctuation but keeps letters, digits, hyphens", () => {
		expect(headingSlug("What's new in v2.0?")).toBe("whats-new-in-v20");
		expect(headingSlug("A – dash — test")).toBe("a-dash-test"); // en/em dashes stripped
		expect(headingSlug("pre-existing-hyphens")).toBe("pre-existing-hyphens");
	});

	it("keeps unicode letters", () => {
		expect(headingSlug("Café Notes")).toBe("café-notes");
		expect(headingSlug("日本語 見出し")).toBe("日本語-見出し");
	});

	it("matches the fragment for a heading link", () => {
		// The plugin slugifies both heading text (ids) and link fragments —
		// the two must agree.
		const heading = "Deploying with Docker!";
		expect(headingSlug(heading)).toBe(headingSlug("Deploying with Docker"));
	});
});

describe("dedupeSlug", () => {
	it("suffixes repeats", () => {
		const used = new Map<string, number>();
		expect(dedupeSlug("notes", used)).toBe("notes");
		expect(dedupeSlug("notes", used)).toBe("notes-1");
		expect(dedupeSlug("notes", used)).toBe("notes-2");
		expect(dedupeSlug("other", used)).toBe("other");
	});

	it("passes empty slugs through", () => {
		expect(dedupeSlug("", new Map())).toBe("");
	});
});

describe("clamp", () => {
	it("returns short strings unchanged", () => {
		expect(clamp("hello", 10)).toBe("hello");
	});

	it("cuts long strings with an ellipsis inside the budget", () => {
		const out = clamp("a".repeat(300), 200);
		expect(out.length).toBeLessThanOrEqual(200);
		expect(out.endsWith("…")).toBe(true);
	});

	it("trims trailing whitespace before the ellipsis", () => {
		// slice(0, 6) of "words and" is "words " — the space must not survive.
		expect(clamp("words and", 7)).toBe("words…");
	});
});

describe("classList", () => {
	it("accepts strings and YAML lists", () => {
		expect(classList("justify wide")).toBe("justify wide");
		expect(classList(["justify", "wide"])).toBe("justify wide");
	});

	it("drops anything that isn't a plain class token", () => {
		expect(classList('justify" onload="x')).toBe("justify");
		expect(classList("ok <script> .dot two;three")).toBe("ok");
		expect(classList(42)).toBe("");
		expect(classList(undefined)).toBe("");
	});
});
