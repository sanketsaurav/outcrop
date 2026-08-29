import { ItemView, Notice, TFile, WorkspaceLeaf, setIcon } from "obsidian";
import { NoteResponse } from "./api";
import type OutcropPlugin from "./main";
import { rotateNote, shareNote, unshareNote, updateAllShared } from "./publish";
import { ConfirmModal } from "./ui";

export const VIEW_TYPE_SHARES = "outcrop-shares";

export class SharesView extends ItemView {
	constructor(leaf: WorkspaceLeaf, private plugin: OutcropPlugin) {
		super(leaf);
	}

	getViewType() {
		return VIEW_TYPE_SHARES;
	}

	getDisplayText() {
		return "Outcrop shares";
	}

	getIcon() {
		return "share-2";
	}

	async onOpen() {
		await this.refresh();
	}

	async refresh() {
		const el = this.contentEl;
		el.empty();
		el.addClass("outcrop-shares");

		if (!this.plugin.client.configured()) {
			el.createEl("p", { text: "Configure the server URL and API key in Outcrop settings first." });
			return;
		}

		let notes: NoteResponse[];
		try {
			notes = await this.plugin.client.listNotes();
		} catch (e) {
			el.createEl("p", { text: `Couldn't reach the server: ${e instanceof Error ? e.message : e}` });
			return;
		}
		const index = this.plugin.sharedFileIndex();

		const header = el.createDiv({ cls: "outcrop-shares-header" });
		header.createEl("strong", { text: `${notes.length} shared note${notes.length === 1 ? "" : "s"}` });
		const actions = header.createDiv({ cls: "outcrop-shares-header-actions" });
		this.iconButton(actions, "refresh-cw", "Refresh", () => this.refresh());
		const updateAllBtn = actions.createEl("button", { text: "Update all" });
		updateAllBtn.onclick = async () => {
			await updateAllShared(this.plugin);
			await this.refresh();
		};

		if (notes.length === 0) {
			el.createEl("p", { text: "Nothing shared yet. Run “Outcrop: Share current note” on any note." });
			return;
		}

		const table = el.createEl("table", { cls: "outcrop-shares-table" });
		const thead = table.createEl("thead").createEl("tr");
		for (const h of ["Note", "Link", "Updated", "Size", ""]) {
			thead.createEl("th", { text: h });
		}
		const tbody = table.createEl("tbody");

		for (const note of notes) {
			const file = index.get(note.id);
			const tr = tbody.createEl("tr");

			const titleTd = tr.createEl("td", { cls: "outcrop-share-title" });
			if (file) {
				const link = titleTd.createEl("a", { text: note.title });
				link.onclick = (e) => {
					e.preventDefault();
					this.app.workspace.getLeaf(false).openFile(file);
				};
			} else {
				titleTd.setText(note.title);
				titleTd.createSpan({ cls: "outcrop-orphan-badge", text: "orphan" }).setAttr(
					"aria-label",
					"On the server but not found in this vault"
				);
			}
			if (note.protected) {
				titleTd.createSpan({ cls: "outcrop-lock", text: " 🔒" });
			}

			const linkTd = tr.createEl("td");
			const a = linkTd.createEl("a", { text: "/" + note.slug, href: note.url });
			a.setAttr("target", "_blank");
			a.setAttr("rel", "noopener");

			tr.createEl("td", { text: relativeTime(note.updated_at) });
			tr.createEl("td", { text: humanSize(note.size_bytes) });

			const actionsTd = tr.createEl("td", { cls: "outcrop-share-actions" });
			this.iconButton(actionsTd, "copy", "Copy link", async () => {
				await navigator.clipboard.writeText(note.url);
				new Notice("Outcrop: link copied.");
			});
			const passcode = file ? this.plugin.getPasscode(file) : null;
			if (passcode) {
				this.iconButton(actionsTd, "key", "Copy passcode", async () => {
					await navigator.clipboard.writeText(passcode);
					new Notice("Outcrop: passcode copied.");
				});
			}
			if (file) {
				this.iconButton(actionsTd, "upload", "Update now", async () => {
					await shareNote(this.plugin, file, { silent: true });
					new Notice("Outcrop: updated.");
					await this.refresh();
				});
				this.iconButton(actionsTd, "rotate-cw", "Rotate link (old link dies)", () => {
					new ConfirmModal(
						this.app,
						"Rotate share link",
						`“${note.title}” gets a new URL and the current link stops working. Continue?`,
						"Rotate",
						async () => {
							await rotateNote(this.plugin, file);
							await this.refresh();
						}
					).open();
				});
			}
			this.iconButton(actionsTd, "trash-2", "Delete share", () => {
				new ConfirmModal(
					this.app,
					"Delete share",
					`The public link for “${note.title}” stops working immediately. The note in your vault is untouched. Continue?`,
					"Delete",
					async () => {
						if (file) {
							await unshareNote(this.plugin, file, { silent: true });
						} else {
							try {
								await this.plugin.client.deleteNote(note.id);
							} catch (e) {
								new Notice(`Outcrop: delete failed — ${e instanceof Error ? e.message : e}`);
							}
						}
						await this.refresh();
					}
				).open();
			});
		}
	}

	private iconButton(parent: HTMLElement, icon: string, label: string, onClick: () => void) {
		const btn = parent.createEl("button", { cls: "outcrop-icon-button clickable-icon" });
		setIcon(btn, icon);
		btn.setAttr("aria-label", label);
		btn.onclick = onClick;
		return btn;
	}
}

function relativeTime(unix: number): string {
	const s = Math.max(0, Math.floor(Date.now() / 1000 - unix));
	if (s < 60) return "just now";
	if (s < 3600) return `${Math.floor(s / 60)}m ago`;
	if (s < 86400) return `${Math.floor(s / 3600)}h ago`;
	if (s < 30 * 86400) return `${Math.floor(s / 86400)}d ago`;
	return new Date(unix * 1000).toLocaleDateString();
}

function humanSize(bytes: number): string {
	if (bytes < 1024) return `${bytes} B`;
	if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
	return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
}
