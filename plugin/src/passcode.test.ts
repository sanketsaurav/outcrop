import { describe, expect, it } from "vitest";
import { generatePasscode } from "./passcode";

describe("generatePasscode", () => {
	it("produces adjective-noun-NN", () => {
		for (let i = 0; i < 200; i++) {
			expect(generatePasscode()).toMatch(/^[a-z]+-[a-z]+-\d{2}$/);
		}
	});

	it("keeps the number in 10–99", () => {
		for (let i = 0; i < 200; i++) {
			const n = Number(generatePasscode().split("-")[2]);
			expect(n).toBeGreaterThanOrEqual(10);
			expect(n).toBeLessThanOrEqual(99);
		}
	});

	it("survives server-side normalization unchanged", () => {
		// The server lowercases and trims before hashing — generated passcodes
		// must already be in normal form so what the user sees is what unlocks.
		const p = generatePasscode();
		expect(p).toBe(p.toLowerCase().trim());
	});

	it("varies", () => {
		const seen = new Set(Array.from({ length: 50 }, generatePasscode));
		expect(seen.size).toBeGreaterThan(40);
	});
});
