import { defineConfig, loadEnv, type UserConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import type { ClientRequest, IncomingMessage } from "node:http";
import { TLSSocket } from "node:tls";
import path from "path";
import { getSameOriginProxyOrigin } from "./viteProxy.ts";
import { createPublicAssetsPlugin } from "./vitePublicAssets.ts";

type ProxyOptions = Exclude<
  NonNullable<NonNullable<UserConfig["server"]>["proxy"]>[string],
  string
>;

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), "");
  const target = env.FORCAD_BACKEND_URL || "http://127.0.0.1:8080";
  const publicDirectory = path.resolve(import.meta.dirname, "../public");
  const rewriteOrigin = (
    proxyRequest: Pick<ClientRequest, "setHeader">,
    request: IncomingMessage,
  ) => {
    const origin = getSameOriginProxyOrigin(
      request.headers.origin,
      request.headers.host,
      request.socket instanceof TLSSocket ? "https" : "http",
      target,
    );
    if (origin) proxyRequest.setHeader("Origin", origin);
  };

  const createProxy = (ws = false): ProxyOptions => ({
    target,
    changeOrigin: true,
    ...(ws ? { ws: true } : {}),
    configure(server) {
      server.on("proxyReq", rewriteOrigin);
      server.on("proxyReqWs", rewriteOrigin);
    },
  });

  return {
    publicDir: publicDirectory,
    build: {
      copyPublicDir: false,
    },
    server: {
      proxy: {
        "/api/events/ws": createProxy(true),
        "/api": createProxy(),
      },
    },
    plugins: [
      createPublicAssetsPlugin(publicDirectory),
      tailwindcss(),
      react(),
    ],
    resolve: {
      alias: {
        "@": path.resolve(import.meta.dirname, "src"),
        "@app": path.resolve(import.meta.dirname, "src/app"),
        "@shared": path.resolve(import.meta.dirname, "src/shared"),
        "@entities": path.resolve(import.meta.dirname, "src/entities"),
        "@features": path.resolve(import.meta.dirname, "src/features"),
        "@widgets": path.resolve(import.meta.dirname, "src/widgets"),
        "@pages": path.resolve(import.meta.dirname, "src/pages"),
        "@components": path.resolve(import.meta.dirname, "src/components"),
      },
    },
  };
});
