import { App, Modal, Setting } from "obsidian";

/** Simple confirm dialog with a warning-styled CTA. */
export class ConfirmModal extends Modal {
	constructor(
		app: App,
		private heading: string,
		private body: string,
		private cta: string,
		private onConfirm: () => void
	) {
		super(app);
	}

	onOpen() {
		this.titleEl.setText(this.heading);
		this.contentEl.createEl("p", { text: this.body });
		new Setting(this.contentEl)
			.addButton((b) => b.setButtonText("Cancel").onClick(() => this.close()))
			.addButton((b) =>
				b
					.setWarning()
					.setButtonText(this.cta)
					.onClick(() => {
						this.close();
						this.onConfirm();
					})
			);
	}

	onClose() {
		this.contentEl.empty();
	}
}

/** Confirm dialog that requires typing an exact phrase (danger-zone actions). */
export class TypedConfirmModal extends Modal {
	constructor(
		app: App,
		private heading: string,
		private body: string,
		private phrase: string,
		private cta: string,
		private onConfirm: () => void
	) {
		super(app);
	}

	onOpen() {
		this.titleEl.setText(this.heading);
		this.contentEl.createEl("p", { text: this.body });
		let typed = "";
		let confirmBtn: HTMLButtonElement | null = null;
		new Setting(this.contentEl).addText((t) => {
			t.setPlaceholder(this.phrase).onChange((v) => {
				typed = v;
				if (confirmBtn) confirmBtn.disabled = typed !== this.phrase;
			});
		});
		new Setting(this.contentEl)
			.addButton((b) => b.setButtonText("Cancel").onClick(() => this.close()))
			.addButton((b) => {
				b.setWarning()
					.setButtonText(this.cta)
					.onClick(() => {
						if (typed !== this.phrase) return;
						this.close();
						this.onConfirm();
					});
				confirmBtn = b.buttonEl;
				confirmBtn.disabled = true;
			});
	}

	onClose() {
		this.contentEl.empty();
	}
}
