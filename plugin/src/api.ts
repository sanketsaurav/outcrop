import { requestUrl } from "obsidian";
import type { OutcropSettings } from "./settings";

export interface NoteResponse {
	id: string;
	slug: string;
	url: string;
	title: string;
	protected: boolean;
	noindex: boolean;
	size_bytes: number;
	created_at: number;
	updated_at: number;
}

export interface PingResponse {
	version: string;
	notes: number;
	theme_updated_at: number;
}

export interface NotePayload {
	title: string;
	description: string;
	html: string;
	noindex: boolean;
	assets: string[];
	slug?: string;
}

export class ApiError extends Error {
	constructor(
		public status: number,
		message: string,
	) {
		super(message);
	}
}

/** Typed client for the Outcrop server. Uses Obsidian's requestUrl, so it works
 * on mobile and is exempt from CORS. */
export class OutcropClient {
	constructor(private settings: OutcropSettings) {}

	configured(): boolean {
		return Boolean(this.settings.serverUrl && this.settings.apiKey);
	}

	private base(): string {
		return this.settings.serverUrl.replace(/\/+$/, "");
	}

	private async req(
		method: string,
		path: string,
		body?: object | ArrayBuffer,
		contentType?: string,
	) {
		const isBinary = body instanceof ArrayBuffer;
		const resp = await requestUrl({
			url: this.base() + path,
			method,
			throw: false,
			headers: { Authorization: `Bearer ${this.settings.apiKey}` },
			contentType:
				contentType ?? (body !== undefined && !isBinary ? "application/json" : undefined),
			body: isBinary ? body : body !== undefined ? JSON.stringify(body) : undefined,
		});
		if (resp.status >= 400) {
			let msg = `server returned HTTP ${resp.status}`;
			try {
				const j = resp.json;
				if (j && typeof j.error === "string") msg = j.error;
			} catch {
				// non-JSON error body
			}
			throw new ApiError(resp.status, msg);
		}
		return resp;
	}

	async ping(): Promise<PingResponse> {
		return (await this.req("GET", "/api/v1/ping")).json;
	}

	async createNote(payload: NotePayload): Promise<NoteResponse> {
		return (await this.req("POST", "/api/v1/notes", payload)).json;
	}

	async updateNote(id: string, payload: NotePayload): Promise<NoteResponse> {
		return (await this.req("PUT", `/api/v1/notes/${id}`, payload)).json;
	}

	async deleteNote(id: string): Promise<void> {
		await this.req("DELETE", `/api/v1/notes/${id}`);
	}

	async rotate(id: string, slug?: string): Promise<NoteResponse> {
		return (await this.req("POST", `/api/v1/notes/${id}/rotate`, slug ? { slug } : {})).json;
	}

	async setPasscode(id: string, passcode: string): Promise<void> {
		await this.req("PUT", `/api/v1/notes/${id}/passcode`, { passcode });
	}

	async clearPasscode(id: string): Promise<void> {
		await this.req("DELETE", `/api/v1/notes/${id}/passcode`);
	}

	async listNotes(): Promise<NoteResponse[]> {
		const resp = await this.req("GET", "/api/v1/notes");
		return resp.json.notes ?? [];
	}

	async assetExists(hash: string): Promise<boolean> {
		const resp = await requestUrl({
			url: this.base() + `/api/v1/assets/${hash}`,
			method: "HEAD",
			throw: false,
			headers: { Authorization: `Bearer ${this.settings.apiKey}` },
		});
		return resp.status === 204;
	}

	async uploadAsset(hash: string, ext: string, data: ArrayBuffer): Promise<void> {
		await this.req(
			"POST",
			`/api/v1/assets/${hash}?ext=${encodeURIComponent(ext)}`,
			data,
			"application/octet-stream",
		);
	}

	async getTheme(): Promise<{ css: string; js: string; head: string; updated_at: number }> {
		return (await this.req("GET", "/api/v1/theme")).json;
	}

	async putTheme(theme: { css: string; js: string; head: string }): Promise<void> {
		await this.req("PUT", "/api/v1/theme", theme);
	}
}
