import assert from "node:assert/strict";
import test from "node:test";
import { getCombinedSlaPercent, getSlaPercent } from "../src/shared/lib/sla.ts";

test("SLA is unknown before any checks run", () => {
  assert.equal(getSlaPercent(0, 0), null);
  assert.equal(getCombinedSlaPercent([]), null);
  assert.equal(getCombinedSlaPercent([{ checks: 0, checksPassed: 0 }]), null);
});

test("combined SLA is weighted by the number of checks", () => {
  assert.equal(
    getCombinedSlaPercent([
      { checks: 10, checksPassed: 9 },
      { checks: 1, checksPassed: 0 },
    ]),
    900 / 11,
  );
});

test("missing service cells do not dilute measured SLA", () => {
  assert.equal(
    getCombinedSlaPercent([{ checks: 4, checksPassed: 3 }]),
    75,
  );
});

test("passed checks are clamped to valid counts", () => {
  assert.equal(getSlaPercent(4, 9), 100);
  assert.equal(getSlaPercent(4, -2), 0);
});
