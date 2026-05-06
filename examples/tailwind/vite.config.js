import { defineConfig } from "vite";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig({
  plugins: [tailwindcss()],
  build: {
    // Petra serves static/app.css directly, so the example uses a stable name
    // instead of a hashed asset manifest.
    emptyOutDir: false,
    outDir: "static",
    rollupOptions: {
      input: "assets/app.css",
      output: {
        assetFileNames: "app[extname]",
      },
    },
  },
});
