import {
  createReadStream,
  existsSync,
  readFileSync,
  readdirSync,
  realpathSync,
  statSync,
} from "node:fs";
import { basename, isAbsolute, relative, resolve, sep } from "node:path";
import type { Plugin } from "vite";

const LOGO_FILENAME = /^[^/\\]+\.(?:svg|png|jpe?g|webp|gif)$/i;

function hasControlCharacters(value: string): boolean {
  return Array.from(value).some((character) => {
    const code = character.charCodeAt(0);
    return code < 0x20 || code === 0x7f;
  });
}

export function resolveTeamLogoPath(
  logoDirectory: string,
  logoFile: string,
): string | null {
  if (
    !LOGO_FILENAME.test(logoFile) ||
    hasControlCharacters(logoFile) ||
    logoFile === "." ||
    logoFile === ".." ||
    basename(logoFile) !== logoFile
  ) {
    return null;
  }

  return resolveFileWithinDirectory(logoDirectory, logoFile);
}

function resolveFileWithinDirectory(directory: string, filePath: string): string | null {
  if (!filePath || filePath.includes("\\") || hasControlCharacters(filePath)) {
    return null;
  }

  try {
    const realDirectory = realpathSync(directory);
    const realFile = realpathSync(resolve(realDirectory, filePath));
    const relativePath = relative(realDirectory, realFile);

    if (
      !relativePath ||
      relativePath === ".." ||
      relativePath.startsWith(`..${sep}`) ||
      isAbsolute(relativePath) ||
      !statSync(realFile).isFile()
    ) {
      return null;
    }

    return realFile;
  } catch {
    return null;
  }
}

function emitPublicAssets(
  publicDirectory: string,
  emitFile: (asset: { fileName: string; source: Buffer }) => void,
): void {
  const publicRoot = realpathSync(publicDirectory);

  const emitDirectory = (directory: string) => {
    for (const entry of readdirSync(directory, { withFileTypes: true })) {
      const filePath = resolve(directory, entry.name);
      const relativePath = relative(publicRoot, filePath);
      const pathParts = relativePath.split(sep);

      if (pathParts[0] === "teams") continue;

      if (entry.isDirectory()) {
        emitDirectory(filePath);
      } else if (entry.isFile()) {
        emitFile({
          fileName: pathParts.join("/"),
          source: readFileSync(filePath),
        });
      }
    }
  };

  emitDirectory(publicRoot);

  const logoDirectory = resolve(publicRoot, "teams");
  if (!existsSync(logoDirectory)) return;

  for (const logoFile of readdirSync(logoDirectory)) {
    const filePath = resolveTeamLogoPath(logoDirectory, logoFile);
    if (!filePath) continue;

    emitFile({
      fileName: `team-logos/${basename(logoFile)}`,
      source: readFileSync(filePath),
    });
  }
}

export function createPublicAssetsPlugin(publicDirectory: string): Plugin {
  const logoDirectory = resolve(publicDirectory, "teams");

  return {
    name: "forcad-public-assets",
    generateBundle() {
      emitPublicAssets(publicDirectory, (asset) => this.emitFile({
        type: "asset",
        ...asset,
      }));
    },
    configureServer(server) {
      server.middlewares.use((request, response, next) => {
        if (!request.url) return next();

        let pathname: string;
        let decodedPathname: string;
        try {
          pathname = new URL(request.url, "http://vite.local").pathname;
          decodedPathname = decodeURIComponent(pathname);
        } catch {
          response.statusCode = 404;
          response.end("Not found");
          return;
        }

        const publicTeamAssetPrefix = "/teams/";
        if (decodedPathname.startsWith(publicTeamAssetPrefix)) {
          const assetPath = decodedPathname.slice(publicTeamAssetPrefix.length);
          if (resolveFileWithinDirectory(logoDirectory, assetPath)) {
            response.statusCode = 404;
            response.end("Not found");
            return;
          }
        }

        if (pathname !== "/team-logos" && !pathname.startsWith("/team-logos/")) return next();

        if (request.method !== "GET" && request.method !== "HEAD") {
          response.statusCode = 405;
          response.setHeader("Allow", "GET, HEAD");
          response.end();
          return;
        }

        const encodedFile = pathname.slice("/team-logos/".length);
        let logoFile: string;
        try {
          if (!encodedFile || encodedFile.includes("/")) {
            response.statusCode = 404;
            response.end("Not found");
            return;
          }
          logoFile = decodeURIComponent(encodedFile);
        } catch {
          response.statusCode = 404;
          response.end("Not found");
          return;
        }

        if (
          !LOGO_FILENAME.test(logoFile) ||
          hasControlCharacters(logoFile) ||
          logoFile.includes("\\")
        ) {
          response.statusCode = 404;
          response.end("Not found");
          return;
        }

        const filePath = resolveTeamLogoPath(logoDirectory, logoFile);
        if (!filePath || !existsSync(filePath)) {
          response.statusCode = 404;
          response.end("Not found");
          return;
        }
        const extension = logoFile.slice(logoFile.lastIndexOf(".") + 1).toLowerCase();
        const contentType = {
          png: "image/png",
          jpg: "image/jpeg",
          jpeg: "image/jpeg",
          webp: "image/webp",
          gif: "image/gif",
          svg: "image/svg+xml",
        }[extension];
        if (!contentType) {
          response.statusCode = 404;
          response.end("Not found");
          return;
        }
        response.setHeader("Content-Type", contentType);
        response.setHeader("Cache-Control", "no-cache");
        response.setHeader("X-Content-Type-Options", "nosniff");
        if (extension === "svg") {
          response.setHeader(
            "Content-Security-Policy",
            "default-src 'none'; style-src 'unsafe-inline'; sandbox",
          );
        }
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
