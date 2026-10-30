import assert from "node:assert/strict";
import test from "node:test";

test("browser schemas validate without attempting dynamic code generation", async () => {
  const original = globalThis.Function;
  let attempts = 0;
  globalThis.Function = new Proxy(original, {
    apply() {
      attempts++;
      throw new Error("Dynamic code generation is disabled in this fixture");
    },
    construct() {
      attempts++;
      throw new Error("Dynamic code generation is disabled in this fixture");
    },
  });
  try {
    await import("../src/shared/lib/schemaRuntime.ts");
    const { AdminSession } = await import("../src/shared/api/generated/schemas/admin/adminSession.zod.ts");
    const valid = { status: "ok", username: "fixture", csrf_token: "a".repeat(64) };
    assert.equal(AdminSession.safeParse(valid).success, true);
    assert.equal(AdminSession.safeParse({ ...valid, csrf_token: "invalid" }).success, false);
    assert.equal(attempts, 0, "Schema initialization or parsing attempted dynamic code generation");
  } finally {
    globalThis.Function = original;
  }
});
