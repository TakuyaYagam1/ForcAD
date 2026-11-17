import assert from "node:assert/strict";
import test from "node:test";
import { createHistoryRequestQueue } from "../src/features/view-scoreboard/model/historyRequestQueue.ts";

function deferred() {
  let resolve!: () => void;
  const promise = new Promise<void>((done) => {
    resolve = done;
  });
  return {
    promise,
    resolve
  };
}

const tick = () => {
  return new Promise((resolve) => {
    setImmediate(resolve);
  });
};

test("history requests enforce the shared concurrency bound", async () => {
  const request = createHistoryRequestQueue(4);
  const gates = Array.from({
    length: 7
  }, deferred);
  const started: number[] = [];
  const jobs = gates.map((gate, index) => {
    return request(async () => {
      started.push(index);
      await gate.promise;
      return index;
    }, new AbortController().signal);
  });
  await tick();
  assert.deepEqual(started, [0, 1, 2, 3]);
  gates[0].resolve();
  await tick();
  assert.deepEqual(started, [0, 1, 2, 3, 4]);
  gates.forEach((gate) => {
    gate.resolve();
  });
  assert.deepEqual(await Promise.all(jobs), [0, 1, 2, 3, 4, 5, 6]);
});

test("queued cancellation removes work and a failed active request releases its slot", async () => {
  const request = createHistoryRequestQueue(1);
  const gate = deferred();
  const first = request(async () => {
    await gate.promise;
    throw new Error("network failure");
  }, new AbortController().signal);
  const aborted = new AbortController();
  let cancelledWorkStarted = false;
  const second = request(async () => {
    cancelledWorkStarted = true;
  }, aborted.signal);
  const secondResult = assert.rejects(second, {
    name: "AbortError"
  });
  aborted.abort();
  await secondResult;
  let nextStarted = false;
  const third = request(async () => {
    nextStarted = true;
    return "next";
  }, new AbortController().signal);
  const firstResult = assert.rejects(first, /network failure/);
  gate.resolve();
  await firstResult;
  assert.equal(await third, "next");
  assert.equal(cancelledWorkStarted, false);
  assert.equal(nextStarted, true);
});

test("already cancelled requests never start", async () => {
  const request = createHistoryRequestQueue(1);
  const aborted = new AbortController();
  aborted.abort();
  let started = false;
  await assert.rejects(request(async () => {
    started = true;
  }, aborted.signal), {
    name: "AbortError"
  });
  assert.equal(started, false);
});
