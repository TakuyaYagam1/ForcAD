type TimerPhase = "waiting" | "running" | "paused" | "finished" | "unknown";

export interface RoundTimerInput {
  nowMs: number;
  phase: TimerPhase;
  roundTime: number | null;
  currentRoundStart?: number | null;
  legacyRoundStart?: number | null;
  round?: number | null;
  pausedAt?: number | null;
  pausedSeconds?: number;
  roundWaiting?: boolean;
}

export interface RoundTimer {
  elapsedSeconds: number | null;
  remainingSeconds: number | null;
  progress: number | null;
  waitingForResults: boolean;
}

/** A deadline remains complete until a new round timestamp arrives. */
export function roundTimerSeconds(elapsed: number, roundTime: number): number {
  return Math.min(roundTime, Math.max(0, elapsed));
}

function timestampMs(value: number | null | undefined): number | null {
  if (value == null || !Number.isFinite(value) || value <= 0) {
    return null;
  }
  return value < 10_000_000_000 ? value * 1000 : value;
}

export function resolveRoundTimer(input: RoundTimerInput): RoundTimer {
  const unavailable: RoundTimer = {
    elapsedSeconds: null,
    remainingSeconds: null,
    progress: null,
    waitingForResults: false,
  };
  const duration = input.roundTime;
  if (
    (input.phase !== "running" && input.phase !== "paused") ||
    duration == null || !Number.isFinite(duration) || duration <= 0 ||
    !Number.isFinite(input.nowMs) ||
    input.round == null || !Number.isFinite(input.round) || input.round <= 0
  ) {
    return unavailable;
  }

  // Runtime identifies the actual round. A legacy scoreboard snapshot instead
  // identifies the completed round, so its successor starts one duration later.
  const runtimeStart = timestampMs(input.currentRoundStart);
  const legacyStart = timestampMs(input.legacyRoundStart);
  const start = runtimeStart ?? (legacyStart === null ? null : legacyStart + duration * 1000);
  const pause = timestampMs(input.pausedAt);
  if (start === null || (input.phase === "paused" && pause === null)) {
    return unavailable;
  }

  const effectiveNow = input.phase === "paused" ? pause! : input.nowMs;
  const pausedSeconds = Number.isFinite(input.pausedSeconds)
    ? Math.max(0, input.pausedSeconds ?? 0)
    : 0;
  const elapsed = Math.max(0, (effectiveNow - start) / 1000 - pausedSeconds);
  const waitingForResults = input.phase === "running" && (input.roundWaiting === true || elapsed >= duration);
  const elapsedSeconds = waitingForResults ? duration : roundTimerSeconds(elapsed, duration);
  return {
    elapsedSeconds,
    remainingSeconds: Math.ceil(Math.max(0, duration - elapsedSeconds)),
    progress: (elapsedSeconds / duration) * 100,
    waitingForResults,
  };
}

export function formatRoundCountdown(seconds: number | null): string {
  if (seconds === null) {
    return "—:—";
  }
  return `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, "0")}`;
}

export function formatRoundElapsed(timer: RoundTimer): string {
  return formatRoundCountdown(timer.elapsedSeconds === null ? null : Math.floor(timer.elapsedSeconds));
}
