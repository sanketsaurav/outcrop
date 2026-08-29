import { App, Notice, PluginSettingTab, Setting, SettingDefinitionItem } from "obsidian";
import type OutcropPlugin from "./main";
import { TypedConfirmModal } from "./ui";
import { unshareAll } from "./publish";
import defaultCss from "./theme/default.css";
import defaultHead from "./theme/default-head.html";
import { DEFAULT_THEME_JS } from "./theme/default-js";

export interface ThemeSettings {
	css: string;
	js: string;
	head: string;
}

export interface OutcropSettings {
	serverUrl: string;
	apiKey: string;
	autoUpdate: boolean;
	autoUpdateDebounceSec: number;
	linkRipple: boolean;
	unsharedLinkBehavior: "unwrap" | "span";
	titleSource: "filename" | "h1";
	copyOnShare: boolean;
	defaultNoindex: boolean;
	renderDelayMs: number;
	fmPrefix: string;
	theme: ThemeSettings;
	themeDirty: boolean;
}

export const DEFAULT_SETTINGS: OutcropSettings = {
	serverUrl: "",
	apiKey: "",
	autoUpdate: true,
	autoUpdateDebounceSec: 10,
	linkRipple: true,
	unsharedLinkBehavior: "unwrap",
	titleSource: "filename",
	copyOnShare: true,
	defaultNoindex: false,
	renderDelayMs: 300,
	fmPrefix: "share",
	theme: { css: defaultCss, js: DEFAULT_THEME_JS, head: defaultHead },
	themeDirty: true,
};

export const DEFAULT_THEME: ThemeSettings = {
	css: defaultCss,
	js: DEFAULT_THEME_JS,
	head: defaultHead,
};

export class OutcropSettingTab extends PluginSettingTab {
	constructor(
		app: App,
		private plugin: OutcropPlugin,
	) {
		super(app, plugin);
	}

	// Declarative definitions (Obsidian 1.13+) feed the settings search;
	// rendering stays with display() below so older Obsidian versions work.
	getSettingDefinitions(): SettingDefinitionItem[] {
		const { plugin } = this;
		return [
			{
				type: "group",
				heading: "Server",
				items: [
					{
						name: "Server URL",
						desc: "Where your Outcrop server lives.",
						control: { type: "text", key: "serverUrl" },
					},
					{
						name: "API key",
						desc: "The API_KEY configured on your server.",
						control: { type: "text", key: "apiKey" },
					},
				],
			},
			{
				type: "group",
				heading: "Sharing",
				items: [
					{ name: "Update shared notes on save", control: { type: "toggle", key: "autoUpdate" } },
					{
						name: "Auto-update debounce (seconds)",
						control: { type: "number", key: "autoUpdateDebounceSec" },
					},
					{
						name: "Refresh linked notes",
						aliases: ["ripple"],
						control: { type: "toggle", key: "linkRipple" },
					},
					{
						name: "Links to unshared notes",
						control: {
							type: "dropdown",
							key: "unsharedLinkBehavior",
							options: { unwrap: "Show as plain text", span: "Show as styled non-link" },
						},
					},
					{
						name: "Title source",
						control: {
							type: "dropdown",
							key: "titleSource",
							options: { filename: "File name", h1: "First heading, then file name" },
						},
					},
					{ name: "Copy link when sharing", control: { type: "toggle", key: "copyOnShare" } },
					{
						name: "Keep new shares out of search engines",
						aliases: ["noindex"],
						control: { type: "toggle", key: "defaultNoindex" },
					},
					{ name: "Render delay (ms)", control: { type: "number", key: "renderDelayMs" } },
					{ name: "Frontmatter prefix", control: { type: "text", key: "fmPrefix" } },
				],
			},
			{
				type: "group",
				heading: "Theme",
				items: [
					{
						name: "Site CSS",
						aliases: ["theme", "style"],
						control: { type: "textarea", key: "themeCss" },
					},
					{ name: "Site JS", aliases: ["theme"], control: { type: "textarea", key: "themeJs" } },
					{
						name: "Head snippet",
						aliases: ["fonts", "google fonts"],
						control: { type: "textarea", key: "themeHead" },
					},
					{ name: "Push theme to server", action: () => void plugin.pushTheme() },
				],
			},
		];
	}

	getControlValue(key: string): unknown {
		const s = this.plugin.settings;
		if (key === "themeCss") return s.theme.css;
		if (key === "themeJs") return s.theme.js;
		if (key === "themeHead") return s.theme.head;
		return s[key as keyof OutcropSettings];
	}

	async setControlValue(key: string, value: unknown): Promise<void> {
		const s = this.plugin.settings;
		if (key === "themeCss" || key === "themeJs" || key === "themeHead") {
			s.theme[key === "themeCss" ? "css" : key === "themeJs" ? "js" : "head"] = String(value);
			s.themeDirty = true;
		} else {
			(s as unknown as Record<string, unknown>)[key] = value;
		}
		await this.plugin.saveSettings();
	}

	display(): void {
		const { containerEl, plugin } = this;
		containerEl.empty();

		// ---- Server ----
		new Setting(containerEl).setName("Server").setHeading();

		new Setting(containerEl)
			.setName("Server URL")
			.setDesc("Where your Outcrop server lives, e.g. https://notes.example.com")
			.addText((t) =>
				t
					.setPlaceholder("https://notes.example.com")
					.setValue(plugin.settings.serverUrl)
					.onChange(async (v) => {
						plugin.settings.serverUrl = v.trim().replace(/\/+$/, "");
						await plugin.saveSettings();
					}),
			);

		new Setting(containerEl)
			.setName("API key")
			.setDesc("The API_KEY configured on your server.")
			.addText((t) => {
				t.inputEl.type = "password";
				t.setValue(plugin.settings.apiKey).onChange(async (v) => {
					plugin.settings.apiKey = v.trim();
					await plugin.saveSettings();
				});
			});

		new Setting(containerEl)
			.setName("Test connection")
			.setDesc("Checks the URL and API key against the server.")
			.addButton((b) =>
				b.setButtonText("Test").onClick(async () => {
					try {
						const ping = await plugin.client.ping();
						new Notice(
							`Outcrop: connected — server v${ping.version}, ${ping.notes} shared note(s).`,
						);
						await plugin.maybePushDefaultTheme(ping);
					} catch (e) {
						new Notice(
							`Outcrop: connection failed — ${e instanceof Error ? e.message : String(e)}`,
						);
					}
				}),
			);

		// ---- Sharing ----
		new Setting(containerEl).setName("Sharing").setHeading();

		new Setting(containerEl)
			.setName("Update shared notes on save")
			.setDesc("Automatically push edits to shared notes after you stop typing.")
			.addToggle((t) =>
				t.setValue(plugin.settings.autoUpdate).onChange(async (v) => {
					plugin.settings.autoUpdate = v;
					await plugin.saveSettings();
				}),
			);

		new Setting(containerEl)
			.setName("Auto-update debounce (seconds)")
			.setDesc("How long to wait after the last edit before pushing.")
			.addText((t) =>
				t.setValue(String(plugin.settings.autoUpdateDebounceSec)).onChange(async (v) => {
					const n = Number(v);
					if (Number.isFinite(n) && n >= 1 && n <= 600) {
						plugin.settings.autoUpdateDebounceSec = n;
						await plugin.saveSettings();
					}
				}),
			);

		new Setting(containerEl)
			.setName("Refresh linked notes")
			.setDesc(
				"When a note is shared, unshared, or its link rotates, re-publish shared notes that link to it so cross-links stay correct.",
			)
			.addToggle((t) =>
				t.setValue(plugin.settings.linkRipple).onChange(async (v) => {
					plugin.settings.linkRipple = v;
					await plugin.saveSettings();
				}),
			);

		new Setting(containerEl)
			.setName("Links to unshared notes")
			.setDesc("What happens to wikilinks pointing at notes that aren't shared.")
			.addDropdown((d) =>
				d
					.addOption("unwrap", "Show as plain text")
					.addOption("span", "Show as styled non-link")
					.setValue(plugin.settings.unsharedLinkBehavior)
					.onChange(async (v) => {
						plugin.settings.unsharedLinkBehavior = v as "unwrap" | "span";
						await plugin.saveSettings();
					}),
			);

		new Setting(containerEl)
			.setName("Title source")
			.setDesc(
				"Where the published title comes from (a `title` or `share_title` frontmatter property always wins).",
			)
			.addDropdown((d) =>
				d
					.addOption("filename", "File name")
					.addOption("h1", "First heading, then file name")
					.setValue(plugin.settings.titleSource)
					.onChange(async (v) => {
						plugin.settings.titleSource = v as "filename" | "h1";
						await plugin.saveSettings();
					}),
			);

		new Setting(containerEl).setName("Copy link when sharing").addToggle((t) =>
			t.setValue(plugin.settings.copyOnShare).onChange(async (v) => {
				plugin.settings.copyOnShare = v;
				await plugin.saveSettings();
			}),
		);

		new Setting(containerEl)
			.setName("Keep new shares out of search engines")
			.setDesc(
				"Sets noindex on new shares by default, keeping them out of the sitemap and search results. Turn on if you mostly share unlisted links. A share_noindex property overrides this per note.",
			)
			.addToggle((t) =>
				t.setValue(plugin.settings.defaultNoindex).onChange(async (v) => {
					plugin.settings.defaultNoindex = v;
					await plugin.saveSettings();
				}),
			);

		new Setting(containerEl)
			.setName("Render delay (ms)")
			.setDesc(
				"Extra time for other plugins (Dataview, Mermaid…) to finish rendering before capture. Raise it if dynamic content is missing from shared notes.",
			)
			.addText((t) =>
				t.setValue(String(plugin.settings.renderDelayMs)).onChange(async (v) => {
					const n = Number(v);
					if (Number.isFinite(n) && n >= 0 && n <= 10000) {
						plugin.settings.renderDelayMs = n;
						await plugin.saveSettings();
					}
				}),
			);

		new Setting(containerEl)
			.setName("Frontmatter prefix")
			.setDesc(
				'Prefix for the plugin\'s frontmatter properties (default "share" → share_id, share_url, …). Changing this orphans existing properties — rename them yourself.',
			)
			.addText((t) =>
				t.setValue(plugin.settings.fmPrefix).onChange(async (v) => {
					const clean = v.trim();
					if (/^[a-zA-Z][a-zA-Z0-9_-]*$/.test(clean)) {
						plugin.settings.fmPrefix = clean;
						await plugin.saveSettings();
					}
				}),
			);

		// ---- Theme ----
		new Setting(containerEl).setName("Theme").setHeading();
		containerEl.createEl("p", {
			cls: "outcrop-theme-help",
			text:
				"These three pieces are served on every public note page: site.css, site.js, and an HTML snippet injected into <head>. " +
				"Start by editing the design tokens at the top of the CSS. Google Fonts go in the head snippet (there's a commented example) — " +
				"the server only allows fonts.googleapis.com / fonts.gstatic.com as third-party origins.",
		});

		this.themeEditor(containerEl, "Site CSS", "css");
		this.themeEditor(containerEl, "Site JS", "js");
		this.themeEditor(containerEl, "Head snippet", "head");

		new Setting(containerEl)
			.setName("Push theme to server")
			.setDesc(
				plugin.settings.themeDirty ? "⚠ Local theme edits not pushed yet." : "Theme is in sync.",
			)
			.addButton((b) =>
				b
					.setCta()
					.setButtonText("Push theme")
					.onClick(async () => {
						await plugin.pushTheme();
						this.display();
					}),
			);

		// ---- Danger zone ----
		new Setting(containerEl).setName("Danger zone").setHeading();
		new Setting(containerEl)
			.setName("Unshare all notes")
			.setDesc(
				"Deletes every share from the server and removes share properties from your notes. Public links stop working immediately.",
			)
			.addButton((b) =>
				b
					.setWarning()
					.setButtonText("Unshare all…")
					.onClick(() => {
						new TypedConfirmModal(
							this.app,
							"Unshare all notes",
							"This deletes every shared note from the server. All public links die immediately. Type “unshare everything” to confirm.",
							"unshare everything",
							"Unshare all",
							() => unshareAll(plugin),
						).open();
					}),
			);
	}

	private themeEditor(containerEl: HTMLElement, name: string, key: keyof ThemeSettings) {
		const { plugin } = this;
		const setting = new Setting(containerEl).setName(name).setClass("outcrop-theme-setting");
		setting.addButton((b) =>
			b.setButtonText("Reset to default").onClick(async () => {
				plugin.settings.theme[key] = DEFAULT_THEME[key];
				plugin.settings.themeDirty = true;
				await plugin.saveSettings();
				this.display();
			}),
		);
		setting.addTextArea((t) => {
			t.inputEl.rows = key === "css" ? 18 : 8;
			t.inputEl.addClass("outcrop-theme-textarea");
			t.setValue(plugin.settings.theme[key]).onChange(async (v) => {
				plugin.settings.theme[key] = v;
				plugin.settings.themeDirty = true;
				await plugin.saveSettings();
			});
		});
	}
}
