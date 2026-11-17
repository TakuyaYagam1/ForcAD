import assert from "node:assert/strict";
import test from "node:test";
import { diffScoreMotion, snapshotScoreMotion } from "../src/features/view-scoreboard/model/scoreboardMotion.ts";

const teams = [{
  id: "a",
  score: 20
}, {
  id: "b",
  score: 10
}];
const snapshot = (rows = teams, generation = 1, resetPending = false, tasks = ["task"]) => {
  return snapshotScoreMotion(rows, tasks, generation, resetPending)
};
const initial = snapshot();

test("initial and identical payloads do not animate", () => {
  for (const previous of [null, initial, snapshot([])]) {
    const change = diffScoreMotion(previous, snapshot(structuredClone(teams)));
    assert.equal(change.deltas.size, 0);
    assert.equal(change.moved.size, 0);
    assert.equal(change.leader, null);
  }
});

test("signed deltas compare immediately previous finite scores and round before sign", () => {
  const next = snapshot([{
    id: "a",
    score: 35
  }, {
    id: "b",
    score: 8.375
  }]);
  assert.deepEqual([...diffScoreMotion(initial, next).deltas], [["a", 15], ["b", -1.63]]);
  assert.deepEqual([...diffScoreMotion(next, snapshot([{
    id: "a",
    score: 36
  }, {
    id: "b",
    score: 8.374
  }])).deltas], [["a", 1]]);
  assert.equal(diffScoreMotion(initial, snapshot([{
    id: "a",
    score: NaN
  }])).deltas.size, 0);
  assert.equal(diffScoreMotion(snapshot([{
    id: "a",
    score: Infinity
  }]), initial).deltas.size, 0);
});

test("new generation, reset transitions and changed task scope establish baseline", () => {
  for (const next of [snapshot([...teams].reverse(), 2), snapshot([...teams].reverse(), 1, true), snapshot([...teams].reverse(), 1, false, ["other"])]) {
    const change = diffScoreMotion(initial, next);
    assert.equal(change.deltas.size, 0);
    assert.equal(change.moved.size, 0);
    assert.equal(change.leader, null);
  }
  assert.equal(diffScoreMotion(snapshot(teams, 1, true), snapshot([{
    id: "a",
    score: 200
  }], 1, true)).deltas.size, 0);
  assert.equal(diffScoreMotion(snapshot(teams, 1, true), snapshot([{
    id: "a",
    score: 200
  }])).deltas.size, 0);
});

test("rank overtakes animate existing keyed rows and signal the new leader", () => {
  const change = diffScoreMotion(initial, snapshot([...teams].reverse()));
  assert.deepEqual([...change.moved], ["b", "a"]);
  assert.equal(change.leader, "b");
  assert.equal(change.deltas.size, 0);
});

test("added teams have no entry delta or leader signal; removed teams have no output", () => {
  const added = {
    id: "c",
    score: 100
  };
  const change = diffScoreMotion(initial, snapshot([added, ...teams]));
  assert.equal(change.deltas.size, 0);
  assert.deepEqual([...change.moved], ["a", "b"]);
  assert.equal(change.leader, null);
  const removed = diffScoreMotion(initial, snapshot([teams[0]]));
  assert.equal(removed.deltas.size, 0);
  assert.equal(removed.moved.size, 0);
});

test("half-cent differences round symmetrically and floating noise stays silent", () => {
  const before = snapshot([{
    id: "a",
    score: 0
  }, {
    id: "b",
    score: 0
  }]);
  assert.deepEqual([...diffScoreMotion(before, snapshot([{
    id: "a",
    score: 0.005
  }, {
    id: "b",
    score: -0.005
  }])).deltas], [["a", 0.01], ["b", -0.01]]);
  assert.equal(diffScoreMotion(snapshot([{
    id: "a",
    score: 0.3
  }]), snapshot([{
    id: "a",
    score: 0.1 + 0.2
  }])).deltas.size, 0);
});
