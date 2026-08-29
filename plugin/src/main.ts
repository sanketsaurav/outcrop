import { Menu, Notice, Plugin, TFile } from "obsidian";
import { OutcropClient, PingResponse } from "./api";
import {
	protectNote,
	removePasscode,
	rotateNote,
	shareNote,
	unshareNote,
	updateAllShared,
} from "./publish";
import {
	DEFAULT_SETTINGS,
	OutcropSettingTab,
	OutcropSettings,
} from "./settings";
import { SharesView, VIEW_TYPE_SHARES } from "./shares-view";
import { ConfirmModal } from "./ui";

/** Frontmatter property names, derived from the configurable prefix. */
export interface FrontmatterProps {
	id: string;
	url: string;
	slug: string;
	passcode: string;
	title: string;
	description: string;
	noindex: string;
}

export default class OutcropPlugin extends Plugin {
	settings!: OutcropSettings;
	client!: OutcropClient;
	private statusEl: HTMLElement | null = null;
	private updateTimers = new Map<string, number>();

	get props(): FrontmatterProps {
		const p = this.settings.fmPrefix;
		return {
			id: `${p}_id`,
			url: `${p}_url`,
			slug: `${p}_slug`,
			passcode: `${p}_passcode`,
			title: `${p}_title`,
			description: `${p}_description`,
			noindex: `${p}_noindex`,
		};
	}

	async onload() {
		await this.loadSettings();
		this.client = new OutcropClient(this.settings);

		this.registerView(VIEW_TYPE_SHARES, (leaf) => new SharesView(leaf, this));
		this.addSettingTab(new OutcropSettingTab(this.app, this));
		this.addRibbonIcon("share-2", "Outcrop: shared notes", () => this.activateSharesView());

		// ---- Commands ----
		this.addCommand({
			id: "share-note",
			name: "Share current note (create or update)",
			checkCallback: this.mdCommand(() => true, (file) => void shareNote(this, file)),
		});
		this.addCommand({
			id: "copy-link",
			name: "Copy share link",
			checkCallback: this.mdCommand(
				(file) => Boolean(this.getShareUrl(file)),
				async (file) => {
					await navigator.clipboard.writeText(this.getShareUrl(file)!);
					new Notice("Outcrop: link copied.");
				}
			),
		});
		this.addCommand({
			id: "copy-passcode",
			name: "Copy passcode",
			checkCallback: this.mdCommand(
				(file) => Boolean(this.getPasscode(file)),
				async (file) => {
					await navigator.clipboard.writeText(this.getPasscode(file)!);
					new Notice("Outcrop: passcode copied.");
				}
			),
		});
		this.addCommand({
			id: "unshare-note",
			name: "Unshare current note",
			checkCallback: this.mdCommand(
				(file) => Boolean(this.getShareId(file)),
				(file) => this.confirmUnshare(file)
			),
		});
		this.addCommand({
			id: "rotate-link",
			name: "Rotate share link (revoke the old one)",
			checkCallback: this.mdCommand(
				(file) => Boolean(this.getShareId(file)),
				(file) => this.confirmRotate(file)
			),
		});
		this.addCommand({
			id: "protect-note",
			name: "Protect note with a passcode",
			checkCallback: this.mdCommand(() => true, (file) => void protectNote(this, file)),
		});
		this.addCommand({
			id: "remove-passcode",
			name: "Remove passcode",
			checkCallback: this.mdCommand(
				(file) => Boolean(this.getShareId(file)),
				(file) => void removePasscode(this, file)
			),
		});
		this.addCommand({
			id: "update-all",
			name: "Update all shared notes",
			callback: () => void updateAllShared(this),
		});
		this.addCommand({
			id: "open-shares",
			name: "Open shared notes list",
			callback: () => void this.activateSharesView(),
		});
		this.addCommand({
			id: "push-theme",
			name: "Push theme to server",
			callback: () => void this.pushTheme(),
		});

		// ---- Auto-update on save ----
		this.registerEvent(
			this.app.vault.on("modify", (file) => {
				if (!this.settings.autoUpdate || !(file instanceof TFile) || file.extension !== "md") return;
				if (!this.getShareId(file)) return;
				const key = file.path;
				window.clearTimeout(this.updateTimers.get(key));
				this.updateTimers.set(
					key,
					window.setTimeout(() => {
						this.updateTimers.delete(key);
						const f = this.app.vault.getFileByPath(key);
						if (f && this.getShareId(f)) {
							void shareNote(this, f, { silent: true, ripple: false });
						}
					}, this.settings.autoUpdateDebounceSec * 1000)
				);
			})
		);

		// ---- File context menu ----
		this.registerEvent(
			this.app.workspace.on("file-menu", (menu, file) => {
				if (!(file instanceof TFile) || file.extension !== "md") return;
				const shared = Boolean(this.getShareId(file));
				menu.addItem((i) =>
					i
						.setSection("outcrop")
						.setTitle(shared ? "Outcrop: Update shared note" : "Outcrop: Share note")
						.setIcon("share-2")
						.onClick(() => void shareNote(this, file))
				);
				if (!shared) return;
				const passcode = this.getPasscode(file);
				menu.addItem((i) =>
					i
						.setSection("outcrop")
						.setTitle("Outcrop: Copy share link")
						.setIcon("copy")
						.onClick(async () => {
							await navigator.clipboard.writeText(this.getShareUrl(file)!);
							new Notice("Outcrop: link copied.");
						})
				);
				if (passcode) {
					menu.addItem((i) =>
						i
							.setSection("outcrop")
							.setTitle("Outcrop: Copy passcode")
							.setIcon("key")
							.onClick(async () => {
								await navigator.clipboard.writeText(passcode);
								new Notice("Outcrop: passcode copied.");
							})
					);
				}
				menu.addItem((i) =>
					i
						.setSection("outcrop")
						.setTitle(passcode ? "Outcrop: Remove passcode" : "Outcrop: Protect with passcode")
						.setIcon(passcode ? "lock-open" : "lock")
						.onClick(() =>
							passcode ? void removePasscode(this, file) : void protectNote(this, file)
						)
				);
				menu.addItem((i) =>
					i
						.setSection("outcrop")
						.setTitle("Outcrop: Rotate link…")
						.setIcon("rotate-cw")
						.onClick(() => this.confirmRotate(file))
				);
				menu.addItem((i) =>
					i
						.setSection("outcrop")
						.setTitle("Outcrop: Unshare…")
						.setIcon("trash-2")
						.onClick(() => this.confirmUnshare(file))
				);
			})
		);

		// ---- Status bar failsafe (desktop) ----
		this.statusEl = this.addStatusBarItem();
		this.statusEl.addClass("outcrop-status", "mod-clickable");
		this.statusEl.setAttr("aria-label", "Outcrop share actions");
		this.registerDomEvent(this.statusEl, "click", (evt) => this.statusMenu(evt));
		this.registerEvent(this.app.workspace.on("active-leaf-change", () => this.refreshStatusBar()));
		this.registerEvent(
			this.app.metadataCache.on("changed", (file) => {
				if (file.path === this.app.workspace.getActiveFile()?.path) this.refreshStatusBar();
			})
		);
		this.refreshStatusBar();
	}

	onunload() {
		for (const t of this.updateTimers.values()) window.clearTimeout(t);
		this.updateTimers.clear();
	}

	// ---- helpers ----

	fm(file: TFile): Record<string, unknown> | undefined {
		return this.app.metadataCache.getFileCache(file)?.frontmatter;
	}

	getShareId(file: TFile): string | null {
		const v = this.fm(file)?.[this.props.id];
		return typeof v === "string" && v ? v : null;
	}

	getShareUrl(file: TFile): string | null {
		const v = this.fm(file)?.[this.props.url];
		return typeof v === "string" && v ? v : null;
	}

	getPasscode(file: TFile): string | null {
		const v = this.fm(file)?.[this.props.passcode];
		if (typeof v === "string" && v.trim()) return v.trim();
		if (typeof v === "number") return String(v); // YAML parses all-digit passcodes as numbers
		return null;
	}

	sharedFiles(): TFile[] {
		return this.app.vault.getMarkdownFiles().filter((f) => this.getShareId(f));
	}

	sharedFileIndex(): Map<string, TFile> {
		const index = new Map<string, TFile>();
		for (const f of this.sharedFiles()) {
			index.set(this.getShareId(f)!, f);
		}
		return index;
	}

	async activateSharesView() {
		const { workspace } = this.app;
		let leaf = workspace.getLeavesOfType(VIEW_TYPE_SHARES)[0];
		if (!leaf) {
			leaf = workspace.getLeaf(true);
			await leaf.setViewState({ type: VIEW_TYPE_SHARES, active: true });
		} else {
			const view = leaf.view;
			if (view instanceof SharesView) void view.refresh();
		}
		workspace.revealLeaf(leaf);
	}

	async pushTheme() {
		if (!this.client.configured()) {
			new Notice("Outcrop: set the server URL and API key in settings first.");
			return;
		}
		try {
			await this.client.putTheme(this.settings.theme);
			this.settings.themeDirty = false;
			await this.saveSettings();
			new Notice("Outcrop: theme pushed — all shared notes now use it.");
		} catch (e) {
			new Notice(`Outcrop: theme push failed — ${e instanceof Error ? e.message : String(e)}`);
		}
	}

	/** Fresh servers have no theme; seed them with the bundled defaults. */
	async maybePushDefaultTheme(ping: PingResponse) {
		if (ping.theme_updated_at === 0) {
			await this.pushTheme();
		}
	}

	refreshStatusBar() {
		if (!this.statusEl) return;
		const file = this.app.workspace.getActiveFile();
		if (!file || file.extension !== "md" || !this.getShareId(file)) {
			this.statusEl.hide();
			return;
		}
		const fm = this.fm(file);
		const locked = fm && fm[this.props.passcode] ? " 🔒" : "";
		this.statusEl.setText(`⛰ shared${locked}`);
		this.statusEl.show();
	}

	private statusMenu(evt: MouseEvent) {
		const file = this.app.workspace.getActiveFile();
		if (!file || !this.getShareId(file)) return;
		const menu = new Menu();
		menu.addItem((i) =>
			i.setTitle("Update now").setIcon("upload").onClick(() => void shareNote(this, file))
		);
		const passcode = this.getPasscode(file);
		menu.addItem((i) =>
			i
				.setTitle("Copy link")
				.setIcon("copy")
				.onClick(async () => {
					await navigator.clipboard.writeText(this.getShareUrl(file)!);
					new Notice("Outcrop: link copied.");
				})
		);
		if (passcode) {
			menu.addItem((i) =>
				i
					.setTitle("Copy passcode")
					.setIcon("key")
					.onClick(async () => {
						await navigator.clipboard.writeText(passcode);
						new Notice("Outcrop: passcode copied.");
					})
			);
		}
		menu.addItem((i) =>
			i
				.setTitle("Open in browser")
				.setIcon("external-link")
				.onClick(() => window.open(this.getShareUrl(file)!, "_blank"))
		);
		menu.addSeparator();
		menu.addItem((i) =>
			i
				.setTitle(passcode ? "Remove passcode" : "Protect with passcode")
				.setIcon(passcode ? "lock-open" : "lock")
				.onClick(() =>
					passcode ? void removePasscode(this, file) : void protectNote(this, file)
				)
		);
		menu.addItem((i) =>
			i.setTitle("Rotate link…").setIcon("rotate-cw").onClick(() => this.confirmRotate(file))
		);
		menu.addItem((i) =>
			i.setTitle("Unshare…").setIcon("trash-2").onClick(() => this.confirmUnshare(file))
		);
		menu.showAtMouseEvent(evt);
	}

	private confirmRotate(file: TFile) {
		new ConfirmModal(
			this.app,
			"Rotate share link",
			"This note gets a new URL and the current link stops working — anyone holding the old link loses access. Continue?",
			"Rotate",
			() => void rotateNote(this, file)
		).open();
	}

	private confirmUnshare(file: TFile) {
		new ConfirmModal(
			this.app,
			"Unshare note",
			"The public link stops working immediately. The note stays in your vault. Continue?",
			"Unshare",
			() => void unshareNote(this, file)
		).open();
	}

	private mdCommand(
		check: (file: TFile) => boolean,
		action: (file: TFile) => void
	): (checking: boolean) => boolean {
		return (checking: boolean) => {
			const file = this.app.workspace.getActiveFile();
			if (!file || file.extension !== "md" || !check(file)) return false;
			if (!checking) action(file);
			return true;
		};
	}

	async loadSettings() {
		const data = (await this.loadData()) as Partial<OutcropSettings> | null;
		this.settings = {
			...DEFAULT_SETTINGS,
			...data,
			theme: { ...DEFAULT_SETTINGS.theme, ...(data?.theme ?? {}) },
		};
	}

	async saveSettings() {
		await this.saveData(this.settings);
	}
}
