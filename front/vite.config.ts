import { defineConfig, loadEnv } from "vite";
import react from "@vitejs/plugin-react-swc";
import path from "path";

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), "");
  const target = env.FORCAD_BACKEND_URL || "http://127.0.0.1:8080";
  return {
    server: {
      proxy: {
        "/api": { target, changeOrigin: true },
        "/socket.io": { target, changeOrigin: true, ws: true },
      },
    },
    plugins: [react()],
    resolve: {
      alias: {
        "@": path.resolve(__dirname, "src"),
        "@app": path.resolve(__dirname, "src/app"),
        "@shared": path.resolve(__dirname, "src/shared"),
        "@entities": path.resolve(__dirname, "src/entities"),
        "@features": path.resolve(__dirname, "src/features"),
        "@widgets": path.resolve(__dirname, "src/widgets"),
        "@pages": path.resolve(__dirname, "src/pages"),
        "@components": path.resolve(__dirname, "src/components"),
      },
    },
  };
});
