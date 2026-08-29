import { Component, MarkdownRenderer, TFile } from "obsidian";
import type OutcropPlugin from "./main";

export interface AssetUpload {
	hash: string;
	ext: string;
	data: ArrayBuffer;
}

export interface RenderedNote {
	title: string;
	description: string;
	html: string;
	assets: AssetUpload[];
}

const IMAGE_EXTS = new Set(["png", "jpg", "jpeg", "gif", "webp", "avif", "svg"]);
const AUDIO_EXTS = new Set(["mp3", "m4a", "ogg", "wav", "flac"]);
const VIDEO_EXTS = new Set(["mp4", "webm", "mov"]);
const ASSET_EXTS = new Set([...IMAGE_EXTS, ...AUDIO_EXTS, ...VIDEO_EXTS, "pdf", "woff", "woff2"]);

const wait = (ms: number) => new Promise((r) => window.setTimeout(r, ms));

/** Renders a note with Obsidian's own renderer, then post-processes the DOM
 * into clean, self-contained HTML plus the binary assets it references. */
export async function renderNote(plugin: OutcropPlugin, file: TFile): Promise<RenderedNote> {
	const { app, settings } = plugin;
	let md = await app.vault.cachedRead(file);
	const cache = app.metadataCache.getFileCache(file);
	const fm: Record<string, unknown> = cache?.frontmatter ?? {};

	// Frontmatter is never published — slice it off before rendering.
	const fmPos = cache?.frontmatterPosition;
	if (fmPos) {
		md = md.slice(fmPos.end.offset).replace(/^\r?\n/, "");
	}

	const container = document.body.createDiv({ cls: "outcrop-render" });
	container.setCssStyles({ position: "absolute", left: "-99999px", top: "0", width: "800px" });
	const component = new Component();
	component.load();
	try {
		await MarkdownRenderer.render(app, md, container, file.path, component);
		if (settings.renderDelayMs > 0) {
			await wait(settings.renderDelayMs); // async postprocessors: Mermaid, Dataview…
		}

		const assets = new Map<string, AssetUpload>();
		await transform(plugin, container, file, assets);

		const title = noteTitle(plugin, file, fm, container);
		const description = noteDescription(fm, plugin, container);
		return {
			title,
			description,
			html: container.innerHTML,
			assets: [...assets.values()],
		};
	} finally {
		component.unload();
		container.remove();
	}
}

async function transform(
	plugin: OutcropPlugin,
	root: HTMLElement,
	file: TFile,
	assets: Map<string, AssetUpload>
) {
	// 1. Obsidian editing chrome that has no business on a public page.
	root
		.querySelectorAll(
			".copy-code-button, .edit-block-button, .collapse-indicator, .heading-collapse-indicator, " +
				".frontmatter, .mod-frontmatter, .frontmatter-container, .metadata-container, " +
				".markdown-embed-link, .markdown-embed-copy, .internal-embed > .file-embed-title"
		)
		.forEach((el) => el.remove());

	// 2. Media embeds → content-addressed asset URLs.
	for (const embed of Array.from(root.querySelectorAll<HTMLElement>(".internal-embed"))) {
		await processEmbed(plugin, embed, file, assets);
	}
	// Anything still pointing into the app (unresolved embeds) can't work publicly.
	for (const el of Array.from(root.querySelectorAll<HTMLElement>("img, audio, video, source"))) {
		const src = el.getAttribute("src") ?? "";
		if (src.startsWith("app://") || src.startsWith("capacitor://")) {
			el.remove();
		}
	}

	// 3. Internal links.
	for (const a of Array.from(root.querySelectorAll<HTMLAnchorElement>("a.internal-link"))) {
		rewriteInternalLink(plugin, a, file);
	}
	// Tag links point nowhere public — keep the pill, drop the link.
	for (const a of Array.from(root.querySelectorAll<HTMLAnchorElement>("a.tag"))) {
		a.replaceWith(createSpan({ cls: "tag", text: a.textContent ?? "" }));
	}

	// 4. Stable heading ids for deep links.
	const used = new Map<string, number>();
	for (const h of Array.from(root.querySelectorAll<HTMLElement>("h1, h2, h3, h4, h5, h6"))) {
		const id = dedupeSlug(headingSlug(h.textContent ?? ""), used);
		if (id) h.setAttribute("id", id);
	}

	// 5. Math, best effort: clone MathJax's document-level styles into the payload.
	if (root.querySelector("mjx-container, .math")) {
		const mjx = document.querySelector("style[id^='MJX'], #MJX-CHTML-styles");
		if (mjx) root.prepend(mjx.cloneNode(true));
	}

	// 6. Sanitize. The server's CSP is the backstop; this keeps the payload clean.
	root.querySelectorAll("script, iframe, object, embed").forEach((el) => el.remove());
	for (const el of Array.from(root.querySelectorAll<HTMLElement>("*"))) {
		for (const attr of Array.from(el.attributes)) {
			if (attr.name.toLowerCase().startsWith("on")) el.removeAttribute(attr.name);
		}
		const href = el.getAttribute("href");
		if (href && href.trim().toLowerCase().startsWith("javascript:")) {
			el.removeAttribute("href");
		}
	}
}

async function processEmbed(
	plugin: OutcropPlugin,
	embed: HTMLElement,
	sourceFile: TFile,
	assets: Map<string, AssetUpload>
) {
	// Note embeds (![[Other note]]) are already rendered inline — keep the content.
	if (embed.classList.contains("markdown-embed")) return;

	const linkText = embed.getAttribute("src");
	if (!linkText) return;
	const target = plugin.app.metadataCache.getFirstLinkpathDest(linkText.split("#")[0], sourceFile.path);
	if (!target) {
		embed.replaceWith(createSpan({ text: embed.textContent ?? linkText }));
		return;
	}
	const ext = target.extension.toLowerCase();
	if (!ASSET_EXTS.has(ext)) {
		embed.replaceWith(createSpan({ cls: "outcrop-unsupported-embed", text: target.name }));
		return;
	}

	const data = await plugin.app.vault.readBinary(target);
	const hash = await sha256Hex(data);
	assets.set(hash, { hash, ext, data });
	const url = `/a/${hash}.${ext}`;

	let el: HTMLElement;
	if (IMAGE_EXTS.has(ext)) {
		el = createEl("img", { attr: { src: url, alt: target.basename } });
		const width = embed.querySelector("img")?.getAttribute("width");
		if (width) el.setAttribute("width", width); // ![[img.png|300]]
	} else if (AUDIO_EXTS.has(ext)) {
		el = createEl("audio", { attr: { src: url, controls: "" } });
	} else if (VIDEO_EXTS.has(ext)) {
		el = createEl("video", { attr: { src: url, controls: "" } });
	} else {
		el = createEl("a", { attr: { href: url }, text: target.name });
	}
	embed.replaceWith(el);
}

function rewriteInternalLink(plugin: OutcropPlugin, a: HTMLAnchorElement, sourceFile: TFile) {
	const linkText = a.getAttribute("data-href") ?? a.getAttribute("href") ?? "";
	const hashIdx = linkText.indexOf("#");
	const pathPart = hashIdx === -1 ? linkText : linkText.slice(0, hashIdx);
	const fragment = hashIdx === -1 ? "" : linkText.slice(hashIdx + 1);

	const target = pathPart
		? plugin.app.metadataCache.getFirstLinkpathDest(pathPart, sourceFile.path)
		: sourceFile; // [[#Heading]] links point at this note

	// Same-note heading links become plain fragments — they work regardless of
	// what this note's public URL turns out to be.
	if (target && target.path === sourceFile.path) {
		a.setAttribute("href", "#" + headingSlug(fragment.replace(/^\^/, "")));
		stripObsidianLinkAttrs(a);
		return;
	}

	const shareUrl = target ? plugin.getShareUrl(target) : null;
	if (shareUrl) {
		a.setAttribute("href", shareUrl + (fragment ? "#" + headingSlug(fragment) : ""));
		stripObsidianLinkAttrs(a);
		return;
	}

	// Target isn't shared.
	if (plugin.settings.unsharedLinkBehavior === "span") {
		a.replaceWith(createSpan({ cls: "unshared-link", text: a.textContent ?? "" }));
	} else {
		a.replaceWith(document.createTextNode(a.textContent ?? ""));
	}
}

function stripObsidianLinkAttrs(a: HTMLAnchorElement) {
	a.removeAttribute("data-href");
	a.removeAttribute("data-tooltip-position");
	a.removeAttribute("aria-label");
	a.removeAttribute("target");
	a.removeAttribute("rel");
}

function noteTitle(
	plugin: OutcropPlugin,
	file: TFile,
	fm: Record<string, unknown>,
	root: HTMLElement
): string {
	const explicit = fm[plugin.props.title] ?? fm["title"];
	if (typeof explicit === "string" && explicit.trim()) return explicit.trim();
	if (plugin.settings.titleSource === "h1") {
		const h1 = root.querySelector("h1")?.textContent?.trim();
		if (h1) return h1;
	}
	return file.basename;
}

function noteDescription(
	fm: Record<string, unknown>,
	plugin: OutcropPlugin,
	root: HTMLElement
): string {
	const explicit = fm[plugin.props.description] ?? fm["description"];
	if (typeof explicit === "string" && explicit.trim()) return clamp(explicit.trim());
	for (const p of Array.from(root.querySelectorAll("p"))) {
		const text = p.textContent?.trim();
		if (text) return clamp(text);
	}
	return "";
}

function clamp(s: string, max = 200): string {
	return s.length <= max ? s : s.slice(0, max - 1).trimEnd() + "…";
}

export function headingSlug(text: string): string {
	return text
		.toLowerCase()
		.trim()
		.replace(/[^\p{L}\p{N}\s-]/gu, "")
		.replace(/\s+/g, "-");
}

function dedupeSlug(slug: string, used: Map<string, number>): string {
	if (!slug) return slug;
	const n = used.get(slug) ?? 0;
	used.set(slug, n + 1);
	return n === 0 ? slug : `${slug}-${n}`;
}

export async function sha256Hex(data: ArrayBuffer): Promise<string> {
	const digest = await crypto.subtle.digest("SHA-256", data);
	return [...new Uint8Array(digest)].map((b) => b.toString(16).padStart(2, "0")).join("");
}
