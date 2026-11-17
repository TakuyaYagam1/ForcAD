import assert from "node:assert/strict";
import test from "node:test";
import { formatRoundCountdown, formatRoundElapsed, resolveRoundTimer, roundTimerSeconds } from "../src/features/view-scoreboard/model/roundTimer.ts";
import type { RoundTimerInput } from "../src/features/view-scoreboard/model/roundTimer.ts";

const start = 1_791_719_200;
const running: RoundTimerInput = {
  nowMs: (start + 30) * 1000,
  phase: "running",
  roundTime: 60,
  currentRoundStart: start,
  legacyRoundStart: start - 60,
  round: 2,
};

test("round elapsed stays complete past the deadline instead of wrapping", () => {
  assert.equal(roundTimerSeconds(59, 60), 59);
  assert.equal(roundTimerSeconds(60, 60), 60);
  assert.equal(roundTimerSeconds(61, 60), 60);
  assert.equal(roundTimerSeconds(120, 60), 60);
  assert.equal(roundTimerSeconds(-5, 60), 0);
  for (const seconds of [60, 61, 120, 600]) {
    assert.deepEqual(resolveRoundTimer({
      ...running,
      nowMs: (start + seconds) * 1000
    }), {
      elapsedSeconds: 60,
      remainingSeconds: 0,
      progress: 100,
      waitingForResults: true,
    });
  }
});

test("runtime timestamp wins over an old scoreboard snapshot and starts a new countdown", () => {
  assert.deepEqual(resolveRoundTimer(running), {
    elapsedSeconds: 30,
    remainingSeconds: 30,
    progress: 50,
    waitingForResults: false,
  });
  assert.deepEqual(resolveRoundTimer({
    ...running,
    currentRoundStart: start + 30,
    round: 3
  }), {
    elapsedSeconds: 0,
    remainingSeconds: 60,
    progress: 0,
    waitingForResults: false,
  });
  assert.equal(resolveRoundTimer({
    ...running,
    legacyRoundStart: start - 600
  }).progress, 50);
});

test("legacy snapshot timestamp describes the completed round and advances one duration", () => {
  assert.deepEqual(resolveRoundTimer({
    ...running,
    currentRoundStart: null
  }), resolveRoundTimer(running));
  assert.equal(resolveRoundTimer({
    ...running,
    currentRoundStart: null,
    nowMs: (start + 80) * 1000
  }).progress, 100);
});

test("pause freezes time and resume excludes the accumulated pause exactly once", () => {
  const paused = {
    ...running,
    phase: "paused" as const,
    pausedAt: start + 35,
    pausedSeconds: 5
  };
  assert.deepEqual(resolveRoundTimer({
    ...paused,
    nowMs: (start + 120) * 1000
  }), {
    elapsedSeconds: 30,
    remainingSeconds: 30,
    progress: 50,
    waitingForResults: false,
  });
  assert.deepEqual(resolveRoundTimer({
    ...paused,
    nowMs: (start + 600) * 1000
  }), resolveRoundTimer(paused));
  assert.deepEqual(resolveRoundTimer({
    ...running,
    nowMs: (start + 125) * 1000,
    pausedSeconds: 90
  }), {
    elapsedSeconds: 35,
    remainingSeconds: 25,
    progress: 35 / 60 * 100,
    waitingForResults: false,
  });
});

test("server waiting flag is authoritative, and the deadline also detects waiting before a poll", () => {
  assert.deepEqual(resolveRoundTimer({
    ...running,
    roundWaiting: true
  }), {
    elapsedSeconds: 60,
    remainingSeconds: 0,
    progress: 100,
    waitingForResults: true,
  });
  assert.equal(resolveRoundTimer({
    ...running,
    nowMs: (start + 60) * 1000,
    roundWaiting: false
  }).waitingForResults, true);
  assert.equal(resolveRoundTimer({
    ...running,
    phase: "paused",
    pausedAt: start + 60,
    roundWaiting: true
  }).waitingForResults, false);
});

test("waiting, finished and unavailable states do not invent a ticking countdown", () => {
  for (const phase of ["waiting", "finished", "unknown"] as const) {
    assert.deepEqual(resolveRoundTimer({
      ...running,
      phase
    }), {
      elapsedSeconds: null,
      remainingSeconds: null,
      progress: null,
      waitingForResults: false,
    });
  }
  for (const override of [
    {
      currentRoundStart: null,
      legacyRoundStart: null
    },
    {
      currentRoundStart: Number.NaN,
      legacyRoundStart: null
    },
    {
      roundTime: null
    }, {
      roundTime: 0
    }, {
      roundTime: -10
    }, {
      roundTime: Number.NaN
    },
    {
      round: 0
    }, {
      round: null
    }, {
      nowMs: Number.NaN
    },
    {
      phase: "paused" as const,
      pausedAt: null
    },
  ]) {
    assert.equal(resolveRoundTimer({
      ...running,
      ...override
    }).progress, null);
  }
});

test("seconds and millisecond timestamps agree, including fractional pause timestamps", () => {
  const seconds = {
    ...running,
    phase: "paused" as const,
    pausedAt: start + 30.5
  };
  const milliseconds = {
    ...seconds,
    currentRoundStart: start * 1000,
    pausedAt: (start + 30.5) * 1000
  };
  assert.deepEqual(resolveRoundTimer(seconds), resolveRoundTimer(milliseconds));
  assert.equal(resolveRoundTimer(seconds).remainingSeconds, 30);
});

test("future start clamps to zero and a fractional final second never displays zero early", () => {
  assert.equal(resolveRoundTimer({
    ...running,
    nowMs: (start - 5) * 1000
  }).progress, 0);
  const finalSecond = resolveRoundTimer({
    ...running,
    nowMs: (start + 59.75) * 1000
  });
  assert.equal(finalSecond.remainingSeconds, 1);
  assert.ok(finalSecond.progress! < 100);
  assert.equal(finalSecond.waitingForResults, false);
});

test("countdown renders minutes, a padded seconds field, and unavailable time", () => {
  assert.equal(formatRoundCountdown(0), "0:00");
  assert.equal(formatRoundCountdown(1), "0:01");
  assert.equal(formatRoundCountdown(65), "1:05");
  assert.equal(formatRoundCountdown(3600), "60:00");
  assert.equal(formatRoundCountdown(null), "—:—");
});

test("round clock counts up and resets only on the next round", () => {
  const display = (input: RoundTimerInput) => {
    return formatRoundElapsed(resolveRoundTimer(input));
  };
  assert.equal(display({
    ...running,
    nowMs: start * 1000
  }), "0:00");
  assert.equal(display({
    ...running,
    nowMs: (start + 10.8) * 1000
  }), "0:10");
  assert.equal(display({
    ...running,
    nowMs: (start + 80) * 1000
  }), "1:00");
  assert.equal(display({
    ...running,
    round: 3,
    currentRoundStart: start + 80,
    nowMs: (start + 80) * 1000
  }), "0:00");
  assert.equal(display({
    ...running,
    phase: "waiting"
  }), "—:—");
});
