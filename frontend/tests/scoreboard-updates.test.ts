import assert from "node:assert/strict";
import test from "node:test";
import {
  changedScoreboardRows,
  snapshotScoreboard,
} from "../src/features/view-scoreboard/model/scoreboardUpdates.ts";

const teams = [{
  id: "first",
  score: 10
}, {
  id: "second",
  score: 5
}];
const tasks = [{
  id: "service"
}];
const results = [{
  teamId: "first",
  taskId: "service",
  status: 101,
  score: 10,
  checks: 2,
  checksPassed: 2,
  sla: 100,
  stolen: 1,
  lost: 0,
  message: "OK",
}];
const initial = snapshotScoreboard(teams, tasks, results);

test("first result and identical cloned payload do not signal an update", () => {
  assert.deepEqual(changedScoreboardRows(null, initial), []);
  assert.deepEqual(changedScoreboardRows(
    initial,
    snapshotScoreboard(structuredClone(teams), structuredClone(tasks), structuredClone(results)),
  ), []);
  assert.deepEqual(changedScoreboardRows(
    snapshotScoreboard([], tasks, []), initial,
  ), []);
});

test("a new ordering signals both moved ranks, even with identical scores", () => {
  assert.deepEqual(changedScoreboardRows(
    initial,
    snapshotScoreboard([...teams].reverse(), tasks, results),
  ), ["second", "first"]);
});

test("score and service changes only signal the affected team", () => {
  assert.deepEqual(changedScoreboardRows(
    initial,
    snapshotScoreboard([{
      ...teams[0],
      score: 12
    }, teams[1]], tasks, results),
  ), ["first"]);
  assert.deepEqual(changedScoreboardRows(
    initial,
    snapshotScoreboard(teams, tasks, [{
      ...results[0],
      status: 104,
      message: "Down"
    }]),
  ), ["first"]);
});

test("transport result order and invisible service updates do not signal", () => {
  const hidden = {
    ...results[0],
    taskId: "hidden",
    score: 100
  };
  assert.deepEqual(changedScoreboardRows(
    snapshotScoreboard(teams, tasks, [...results, hidden]),
    snapshotScoreboard(teams, tasks, [{
      ...hidden,
      score: 999
    }, ...results]),
  ), []);
});

test("new teams signal while changed visible columns establish a baseline", () => {
  assert.deepEqual(changedScoreboardRows(initial, snapshotScoreboard([
    ...teams, {
      id: "new",
      score: 0
    },
  ], tasks, results)), ["new"]);
  assert.deepEqual(changedScoreboardRows(
    initial,
    snapshotScoreboard(teams, [...tasks, {
      id: "new-service"
    }], results),
  ), []);
});
