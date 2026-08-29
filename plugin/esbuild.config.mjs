import esbuild from "esbuild";
import fs from "node:fs/promises";
import process from "node:process";

const prod = process.argv[2] === "production";

// Theme files under src/theme/ are bundled as raw text — they get pushed to the
// server verbatim, not executed by the plugin.
const themeText = {
	name: "theme-text",
	setup(build) {
		build.onLoad({ filter: /src[\\/]theme[\\/].*\.(css|html)$/ }, async (args) => ({
			contents: await fs.readFile(args.path, "utf8"),
			loader: "text",
		}));
	},
};

const ctx = await esbuild.context({
	entryPoints: ["src/main.ts"],
	outfile: "main.js",
	bundle: true,
	format: "cjs",
	target: "es2021",
	platform: "browser",
	external: ["obsidian", "electron", "@codemirror/*", "@lezer/*"],
	logLevel: "info",
	sourcemap: prod ? false : "inline",
	treeShaking: true,
	plugins: [themeText],
});

if (prod) {
	await ctx.rebuild();
	await ctx.dispose();
	process.exit(0);
} else {
	await ctx.watch();
}
