import { defineConfig } from "vite";

export default defineConfig({
  build: {
    // Petra serves static/app.js directly. Stable filenames keep the template
    // simple and avoid manifest plumbing in this focused example.
    emptyOutDir: false,
    outDir: "static",
    rollupOptions: {
      input: "assets/app.js",
      output: {
        entryFileNames: "app.js",
      },
    },
  },
});
