import assert from "node:assert/strict";
import test from "node:test";
import {
  compareUuid,
  isUuid,
  normalizeUuid,
} from "../src/shared/lib/uuid.ts";

const lowerUuid = "0199f7c0-0000-7000-8000-000000000001";
const laterUuid = "0199f7c0-0000-7000-8000-000000000002";

test("UUID route parsing validates IDs and canonicalizes case", () => {
  assert.equal(isUuid(lowerUuid), true);
  assert.equal(isUuid("3"), false);
  assert.equal(isUuid("not-a-uuid"), false);
  assert.equal(normalizeUuid(lowerUuid.toUpperCase()), lowerUuid);
  assert.equal(normalizeUuid("3"), undefined);
});

test("UUID ordering matches canonical byte order independent of case", () => {
  assert.equal(compareUuid(lowerUuid, laterUuid), -1);
  assert.equal(compareUuid(laterUuid, lowerUuid), 1);
  assert.equal(compareUuid(lowerUuid, lowerUuid.toUpperCase()), 0);
});
