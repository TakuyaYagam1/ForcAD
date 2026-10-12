import { readFile, writeFile } from "node:fs/promises";
import { defineConfig, defineTransformer } from "orval";

const httpMethods = [
  "get",
  "put",
  "post",
  "patch",
  "delete",
  "options",
  "head",
  "trace",
] as const;

type HttpMethod = (typeof httpMethods)[number];
type HttpOperation = { responses?: Record<string, unknown> };
type HttpPathItem = Partial<Record<HttpMethod, HttpOperation>>;
type HttpDocument = {
  paths?: Record<string, HttpPathItem>;
  components?: { responses?: Record<string, unknown> };
};

const transformForHttpClient = defineTransformer((spec) => {
  const document = structuredClone(spec) as unknown as HttpDocument;
  if (!document.paths) return spec;

  delete document.paths["/api/events/ws"];
  const responseComponents = document.components?.responses ?? {};
  for (const pathItem of Object.values(document.paths)) {
    for (const method of httpMethods) {
      const responses = pathItem[method]?.responses;
      if (!responses) continue;

      for (const [status, response] of Object.entries(responses)) {
        if (!response || typeof response !== "object" || Array.isArray(response)) {
          continue;
        }
        const reference = (response as { $ref?: unknown }).$ref;
        if (typeof reference !== "string") continue;
        const responseName = reference.match(
          /^#\/components\/responses\/([^/]+)$/,
        )?.[1];
        if (responseName && responseComponents[responseName]) {
          responses[status] = responseComponents[responseName];
        }
      }
    }
  }

  return document as typeof spec;
});

export default defineConfig({
  forcad: {
    hooks: {
      afterAllFilesWrite: async (paths) => {
        if (!Array.isArray(paths)) throw new TypeError("Expected generated paths");
        for (const filename of paths) {
          if (typeof filename !== "string" || !filename.endsWith(".ts")) continue;
          const source = await readFile(filename, "utf8");
          await writeFile(filename, source.trimEnd() + "\n");
        }
      },
    },
    input: {
      target: "../../api/openapi.yml",
      override: {
        transformer: transformForHttpClient,
      },
    },
    output: {
      mode: "tags-split",
      target: "../src/shared/api/generated/endpoints",
      clean: true,
      indexFiles: true,
      headers: true,
      schemas: {
        path: "../src/shared/api/generated/schemas",
        type: "zod",
        splitByTags: true,
      },
      client: "react-query",
      httpClient: "axios",
      override: {
        mutator: {
          path: "../src/shared/api/axiosMutator.ts",
          name: "axiosMutator",
        },
        query: {
          signal: true,
        },
      },
    },
  },
});
