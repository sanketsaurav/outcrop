// The default site JS, served as /t/site.js on every public note page.
// Kept as a template literal (not a .js file) so the plugin bundles it as text
// without build-config tricks. It runs in visitors' browsers, not in Obsidian.
// Note: it is loaded synchronously in <head>, so the stored theme applies
// before first paint; anything touching the DOM waits for DOMContentLoaded.
export const DEFAULT_THEME_JS = `// Outcrop site.js — runs on every public note page.
// This file is yours: extend it from the plugin's theme settings.

// ---- Theme switcher: light / dark / system, persisted per browser ----
var THEME_KEY = "outcrop-theme";

function storedTheme() {
	try {
		var v = localStorage.getItem(THEME_KEY);
		return v === "light" || v === "dark" ? v : null;
	} catch (e) {
		return null;
	}
}

function applyTheme(mode) {
	if (mode === "light" || mode === "dark") {
		document.documentElement.setAttribute("data-theme", mode);
	} else {
		document.documentElement.removeAttribute("data-theme");
	}
	var pills = document.querySelectorAll(".theme-pill");
	for (var i = 0; i < pills.length; i++) {
		var pill = pills[i];
		var value = pill.getAttribute("data-theme-value") || "system";
		pill.setAttribute("aria-pressed", String(value === (mode || "system")));
	}
}

applyTheme(storedTheme()); // before first paint; pills don't exist yet

document.addEventListener("DOMContentLoaded", function () {
	applyTheme(storedTheme()); // now also marks the active pill

	var pills = document.querySelectorAll(".theme-pill");
	for (var i = 0; i < pills.length; i++) {
		pills[i].addEventListener("click", function (evt) {
			var mode = evt.currentTarget.getAttribute("data-theme-value");
			try {
				if (mode === "system") localStorage.removeItem(THEME_KEY);
				else localStorage.setItem(THEME_KEY, mode);
			} catch (e) { /* private mode etc. — applies for this page only */ }
			applyTheme(mode === "system" ? null : mode);
		});
	}

	// External links open in a new tab; in the footer they also get a small
	// arrow marker via the .external-link class (see site.css).
	var links = document.querySelectorAll(".note-body a[href^='http'], .note-footer a[href^='http']");
	for (var j = 0; j < links.length; j++) {
		if (links[j].hostname !== location.hostname) {
			links[j].target = "_blank";
			links[j].rel = "noopener";
			links[j].classList.add("external-link");
		}
	}

	// Hover anchor links on headings (styled via .heading-anchor in site.css).
	var headings = document.querySelectorAll(".note-body :is(h1,h2,h3,h4,h5,h6)[id]");
	for (var k = 0; k < headings.length; k++) {
		var h = headings[k];
		var a = document.createElement("a");
		a.href = "#" + h.id;
		a.className = "heading-anchor";
		a.textContent = "#";
		a.setAttribute("aria-label", "Link to this section");
		h.appendChild(a);
	}
});
`;
