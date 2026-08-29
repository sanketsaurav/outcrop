import { Notice, TFile } from "obsidian";
import { ApiError, NotePayload, NoteResponse } from "./api";
import type OutcropPlugin from "./main";
import { generatePasscode } from "./passcode";
import { renderNote } from "./render";

export interface ShareOptions {
	silent?: boolean; // no clipboard, only error notices
	ripple?: boolean; // refresh shared notes that link here (default true for state changes)
}

/** Share or update a note: render → upload assets → upsert → sync slug and
 * passcode from frontmatter → write share_id/share_url back. Idempotent. */
export async function shareNote(
	plugin: OutcropPlugin,
	file: TFile,
	opts: ShareOptions = {},
): Promise<NoteResponse | null> {
	if (!plugin.client.configured()) {
		new Notice("Outcrop: set the server URL and API key in settings first.");
		return null;
	}
	try {
		const rendered = await renderNote(plugin, file);

		for (const asset of rendered.assets) {
			if (!(await plugin.client.assetExists(asset.hash))) {
				await plugin.client.uploadAsset(asset.hash, asset.ext, asset.data);
			}
		}

		const props = plugin.props;
		const fm = plugin.fm(file);
		const fmNoindex = fm?.[props.noindex];
		const payload: NotePayload = {
			title: rendered.title,
			description: rendered.description,
			html: rendered.html,
			noindex: typeof fmNoindex === "boolean" ? fmNoindex : plugin.settings.defaultNoindex,
			assets: rendered.assets.map((a) => a.hash),
		};
		const wantSlug = strProp(fm?.[props.slug]);
		const existingId = strProp(fm?.[props.id]);
		const wasShared = Boolean(existingId);

		let note: NoteResponse;
		if (existingId) {
			try {
				note = await plugin.client.updateNote(existingId, payload);
			} catch (e) {
				if (e instanceof ApiError && e.status === 404) {
					// Deleted server-side — recreate and heal the frontmatter.
					note = await createNote(plugin, payload, wantSlug);
				} else {
					throw e;
				}
			}
		} else {
			note = await createNote(plugin, payload, wantSlug);
		}

		// Custom slug drift (share_slug edited after sharing) → rotate to it.
		if (wantSlug && wantSlug !== note.slug) {
			try {
				note = await plugin.client.rotate(note.id, wantSlug);
			} catch (e) {
				if (e instanceof ApiError && e.status === 409) {
					new Notice(`Outcrop: slug "${wantSlug}" is taken — keeping ${note.slug}.`);
				} else {
					throw e;
				}
			}
		}

		// Passcode: frontmatter is authoritative.
		const passcode = strProp(fm?.[props.passcode]);
		if (passcode) {
			await plugin.client.setPasscode(note.id, passcode);
			if (!note.protected && wasShared && !opts.silent) {
				new Notice("Outcrop: passcode protection enabled.");
			}
			note.protected = true;
		} else if (note.protected) {
			await plugin.client.clearPasscode(note.id);
			note.protected = false;
			new Notice("Outcrop: passcode removed (no passcode in frontmatter).");
		}

		await plugin.app.fileManager.processFrontMatter(file, (f) => {
			f[props.id] = note.id;
			f[props.url] = note.url;
		});

		if (!opts.silent) {
			if (plugin.settings.copyOnShare) {
				const clip = note.protected && passcode ? `${note.url}\npasscode: ${passcode}` : note.url;
				await navigator.clipboard.writeText(clip);
				new Notice(`Outcrop: shared — link copied\n${note.url}`);
			} else {
				new Notice(`Outcrop: shared\n${note.url}`);
			}
		}

		plugin.refreshStatusBar();
		if (!wasShared && opts.ripple !== false) {
			await rippleUpdate(plugin, file);
		}
		return note;
	} catch (e) {
		console.error("Outcrop: share failed", e);
		new Notice(`Outcrop: share failed — ${errMsg(e)}`);
		return null;
	}
}

async function createNote(
	plugin: OutcropPlugin,
	payload: NotePayload,
	wantSlug: string | undefined,
): Promise<NoteResponse> {
	if (wantSlug) {
		try {
			return await plugin.client.createNote({ ...payload, slug: wantSlug });
		} catch (e) {
			if (e instanceof ApiError && e.status === 409) {
				new Notice(`Outcrop: slug "${wantSlug}" is taken — using a random link instead.`);
			} else {
				throw e;
			}
		}
	}
	return plugin.client.createNote(payload);
}

export async function unshareNote(plugin: OutcropPlugin, file: TFile, opts: ShareOptions = {}) {
	const props = plugin.props;
	const id = strProp(plugin.fm(file)?.[props.id]);
	if (!id) {
		new Notice("Outcrop: this note isn't shared.");
		return;
	}
	try {
		await plugin.client.deleteNote(id);
	} catch (e) {
		if (!(e instanceof ApiError && e.status === 404)) {
			new Notice(`Outcrop: unshare failed — ${errMsg(e)}`);
			return;
		}
	}
	await plugin.app.fileManager.processFrontMatter(file, (f) => {
		delete f[props.id];
		delete f[props.url];
		delete f[props.passcode];
	});
	if (!opts.silent) new Notice("Outcrop: note unshared — the public link is dead.");
	plugin.refreshStatusBar();
	if (opts.ripple !== false) await rippleUpdate(plugin, file);
}

export async function rotateNote(plugin: OutcropPlugin, file: TFile) {
	const props = plugin.props;
	const id = strProp(plugin.fm(file)?.[props.id]);
	if (!id) {
		new Notice("Outcrop: this note isn't shared.");
		return;
	}
	try {
		const note = await plugin.client.rotate(id);
		await plugin.app.fileManager.processFrontMatter(file, (f) => {
			f[props.url] = note.url;
			delete f[props.slug]; // rotation is a revocation — drop any pinned slug
		});
		await navigator.clipboard.writeText(note.url);
		new Notice(`Outcrop: link rotated — old link is dead. New link copied.\n${note.url}`);
		plugin.refreshStatusBar();
		await rippleUpdate(plugin, file);
	} catch (e) {
		new Notice(`Outcrop: rotate failed — ${errMsg(e)}`);
	}
}

export async function protectNote(plugin: OutcropPlugin, file: TFile) {
	const props = plugin.props;
	let fm = plugin.fm(file);
	if (!strProp(fm?.[props.id])) {
		if (!(await shareNote(plugin, file, { silent: true }))) return;
		fm = plugin.fm(file);
	}
	const passcode = strProp(fm?.[props.passcode]) ?? generatePasscode();
	await plugin.app.fileManager.processFrontMatter(file, (f) => {
		f[props.passcode] = passcode;
	});
	const id = strProp(plugin.fm(file)?.[props.id]);
	const url = strProp(plugin.fm(file)?.[props.url]);
	try {
		await plugin.client.setPasscode(id!, passcode);
		await navigator.clipboard.writeText(`${url}\npasscode: ${passcode}`);
		new Notice(`Outcrop: protected. Passcode: ${passcode}\nLink + passcode copied.`, 8000);
		plugin.refreshStatusBar();
	} catch (e) {
		new Notice(`Outcrop: protect failed — ${errMsg(e)}`);
	}
}

export async function removePasscode(plugin: OutcropPlugin, file: TFile) {
	const props = plugin.props;
	const id = strProp(plugin.fm(file)?.[props.id]);
	if (!id) {
		new Notice("Outcrop: this note isn't shared.");
		return;
	}
	try {
		await plugin.client.clearPasscode(id);
		await plugin.app.fileManager.processFrontMatter(file, (f) => {
			delete f[props.passcode];
		});
		new Notice("Outcrop: passcode removed — the link now opens for everyone.");
		plugin.refreshStatusBar();
	} catch (e) {
		new Notice(`Outcrop: removing passcode failed — ${errMsg(e)}`);
	}
}

/** Re-publish every shared note. Used after theme/structure changes. */
export async function updateAllShared(plugin: OutcropPlugin): Promise<void> {
	const files = plugin.sharedFiles();
	if (files.length === 0) {
		new Notice("Outcrop: no shared notes in this vault.");
		return;
	}
	const progress = new Notice(`Outcrop: updating 0/${files.length}…`, 0);
	let done = 0;
	let failed = 0;
	const queue = [...files];
	const worker = async () => {
		for (let f = queue.shift(); f; f = queue.shift()) {
			const ok = await shareNote(plugin, f, { silent: true, ripple: false });
			if (!ok) failed++;
			done++;
			progress.setMessage(`Outcrop: updating ${done}/${files.length}…`);
		}
	};
	await Promise.all([worker(), worker(), worker()]);
	progress.hide();
	new Notice(
		failed === 0
			? `Outcrop: updated ${done} shared note(s).`
			: `Outcrop: updated ${done - failed}/${done}; ${failed} failed (see console).`,
	);
}

/** Delete every share on the server and clean frontmatter of matching files. */
export async function unshareAll(plugin: OutcropPlugin): Promise<void> {
	try {
		const notes = await plugin.client.listNotes();
		const index = plugin.sharedFileIndex();
		for (const n of notes) {
			await plugin.client.deleteNote(n.id);
			const file = index.get(n.id);
			if (file) {
				await plugin.app.fileManager.processFrontMatter(file, (f) => {
					delete f[plugin.props.id];
					delete f[plugin.props.url];
					delete f[plugin.props.passcode];
				});
			}
		}
		new Notice(`Outcrop: unshared ${notes.length} note(s). All public links are dead.`);
		plugin.refreshStatusBar();
	} catch (e) {
		new Notice(`Outcrop: unshare-all failed — ${errMsg(e)}`);
	}
}

/** Re-publish shared notes that link to changedFile so cross-links stay live. */
export async function rippleUpdate(plugin: OutcropPlugin, changedFile: TFile) {
	if (!plugin.settings.linkRipple) return;
	const resolved = plugin.app.metadataCache.resolvedLinks;
	const targets: TFile[] = [];
	for (const [src, links] of Object.entries(resolved)) {
		if (src === changedFile.path || !links[changedFile.path]) continue;
		const f = plugin.app.vault.getFileByPath(src);
		if (f && plugin.getShareId(f)) targets.push(f);
	}
	for (const f of targets) {
		await shareNote(plugin, f, { silent: true, ripple: false });
	}
	if (targets.length > 0) {
		new Notice(`Outcrop: refreshed ${targets.length} linked note(s).`);
	}
}

function strProp(v: unknown): string | undefined {
	if (typeof v === "string" && v.trim()) return v.trim();
	if (typeof v === "number") return String(v);
	return undefined;
}

function errMsg(e: unknown): string {
	return e instanceof Error ? e.message : String(e);
}
