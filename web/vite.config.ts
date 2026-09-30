import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { VitePWA } from "vite-plugin-pwa";
import { fileURLToPath } from "node:url";

export default defineConfig({
  plugins: [
    react(),
    tailwindcss(),
    VitePWA({
      registerType: "autoUpdate",
      includeAssets: ["icon.svg", "favicon-butterfly.png", "apple-touch-butterfly.png", "logo-butterfly.png"],
      manifest: {
        name: "Viceroy",
        short_name: "Viceroy",
        description: "Self-hosted budgeting",
        theme_color: "#f26b1d",
        background_color: "#f6f5f3",
        display: "standalone",
        start_url: "/",
        // The installed app's icon is fixed at install time, so it doesn't follow the logo toggle.
        icons: [
          { src: "icon-butterfly-192.png", sizes: "192x192", type: "image/png", purpose: "any" },
          { src: "icon-butterfly-512.png", sizes: "512x512", type: "image/png", purpose: "any" },
          { src: "icon-butterfly-maskable-512.png", sizes: "512x512", type: "image/png", purpose: "maskable" },
        ],
      },
      workbox: {
        navigateFallbackDenylist: [/^\/api\//],
        importScripts: ["push-sw.js"],
        runtimeCaching: [],
      },
    }),
  ],
  resolve: { alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) } },
  server: {
    proxy: { "/api": "http://127.0.0.1:8420" },
  },
  build: { emptyOutDir: true },
});
