import assert from "node:assert/strict";
import test from "node:test";
import { getSameOriginProxyOrigin } from "../viteProxy.ts";

const target = "http://127.0.0.1:8080/api";

test("rewrites an origin matching the Vite HTTP host", () => {
  assert.equal(
    getSameOriginProxyOrigin(
      "http://localhost:5173",
      "localhost:5173",
      "http",
      target,
    ),
    "http://127.0.0.1:8080",
  );
});

test("matches the Vite request scheme", () => {
  assert.equal(
    getSameOriginProxyOrigin(
      "https://localhost:5173",
      "localhost:5173",
      "http",
      target,
    ),
    undefined,
  );
});

test("preserves foreign origins", () => {
  assert.equal(
    getSameOriginProxyOrigin(
      "https://attacker.example",
      "localhost:5173",
      "http",
      target,
    ),
    undefined,
  );
});

test("does not treat URLs with paths as serialized origins", () => {
  assert.equal(
    getSameOriginProxyOrigin(
      "http://localhost:5173/other-path",
      "localhost:5173",
      "http",
      target,
    ),
    undefined,
  );
});
