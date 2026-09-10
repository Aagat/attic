import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig({
  root: "marketing",
  base: "/attic/",
  plugins: [react(), tailwindcss()],
  build: { outDir: "../dist-marketing", emptyOutDir: true },
  server: { host: "127.0.0.1", port: 5174, strictPort: true },
  preview: { host: "127.0.0.1", port: 4174, strictPort: true },
});
