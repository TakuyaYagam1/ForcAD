import assert from "node:assert/strict";
import test from "node:test";
import { notificationToLiveEvent } from "../src/entities/live-scoreboard/model/events.ts";
import { randomUuid } from "../src/shared/lib/randomUuid.ts";

test("browser creates valid UUIDs without the secure-context randomUUID API", () => {
  const original = Object.getOwnPropertyDescriptor(globalThis, "crypto")!;
  const getRandomValues = crypto.getRandomValues.bind(crypto);
  Object.defineProperty(globalThis, "crypto", {
    configurable: true,
    value: {
      getRandomValues
    },
  });
  try {
    const ids = Array.from({
      length: 100
    }, () => {
      return randomUuid()
    });
    for (const id of ids) {
      assert.match(id, /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
    }
    assert.equal(new Set(ids).size, ids.length);
    const notification = notificationToLiveEvent({
      attacker_id: "attacker",
      victim_id: "victim",
      task_id: "task",
      attacker_delta: 1,
    });
    assert.match(notification.id, /^[0-9a-f-]{36}$/);

  } finally {
    Object.defineProperty(globalThis, "crypto", original);
  }
});
