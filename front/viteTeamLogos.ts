import { createReadStream, existsSync } from "node:fs";
import { resolve } from "node:path";
import type { Plugin } from "vite";
import { demoTeams } from "./src/app/demo/teamLogos.ts";

const filesByRequestPath = new Map<string, string>(
  demoTeams.map(({ logo }) => [`/team-logos/${logo}`, logo] as const),
);
filesByRequestPath.set("/team-logos/mock_molotok.jpg", "mock_molotok.jpg");

export function resolveTeamLogoPath(logoDirectory: string, logoFile: string) {
  const filePath = resolve(logoDirectory, logoFile);
  if (logoFile === "molotok.jpg" && !existsSync(filePath)) {
    return resolve(logoDirectory, "mock_molotok.jpg");
  }
  return filePath;
}

export function createTeamLogosPlugin(logoDirectory: string): Plugin {
  return {
    name: "forcad-team-logos",
    apply: "serve",
    configureServer(server) {
      server.middlewares.use((request, response, next) => {
        if (!request.url) return next();

        let pathname: string;
        try {
          pathname = new URL(request.url, "http://vite.local").pathname;
        } catch {
          return next();
        }

        if (
          pathname !== "/team-logos" &&
          !pathname.startsWith("/team-logos/")
        ) {
          return next();
        }

        if (request.method !== "GET" && request.method !== "HEAD") {
          response.statusCode = 405;
          response.setHeader("Allow", "GET, HEAD");
          response.end();
          return;
        }

        const logoFile = filesByRequestPath.get(pathname);
        if (!logoFile) {
          response.statusCode = 404;
          response.end("Not found");
          return;
        }

        const filePath = resolveTeamLogoPath(logoDirectory, logoFile);
        if (!existsSync(filePath)) {
          response.statusCode = 404;
          response.end("Not found");
          return;
        }
        response.setHeader(
          "Content-Type",
          logoFile.endsWith(".png") ? "image/png" : "image/jpeg",
        );
        response.setHeader("Cache-Control", "no-cache");
        response.setHeader("X-Content-Type-Options", "nosniff");
        if (request.method === "HEAD") {
          response.end();
          return;
        }

        const stream = createReadStream(filePath);
        stream.on("error", (error: NodeJS.ErrnoException) => {
          if (response.headersSent) {
            response.destroy(error);
            return;
          }
          response.statusCode = error.code === "ENOENT" ? 404 : 500;
          response.end();
        });
        stream.pipe(response);
      });
    },
  };
}
