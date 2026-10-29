import { useScoreboardStore } from "@/entities/scoreboard/model/store";
import { useTranslation } from "react-i18next";
import { useEffect, useState } from "react";
import { roundTimerSeconds } from "../model/roundTimer";

export function StatusesBar({
  round,
  roundStart,
}: {
  round?: number;
  roundStart?: number | null;
}) {
  const { t } = useTranslation("public");
  const roundTime = useScoreboardStore((state) => state.roundTime);
  const runtimeRound = useScoreboardStore((state) => state.runtimeRound);
  const currentRoundStart = useScoreboardStore(
    (state) => state.currentRoundStart,
  );
  const displayRound = runtimeRound ?? round;
  const timerStart = currentRoundStart ?? roundStart;
  const phase = useScoreboardStore((state) => state.phase);
  const roundWaiting = useScoreboardStore((state) => state.roundWaiting);
  const pausedAt = useScoreboardStore((state) => state.pausedAt);
  const pausedSeconds = useScoreboardStore((state) => state.pausedSeconds);
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, []);
  const startMs = timerStart
    ? timerStart < 10_000_000_000
      ? timerStart * 1000
      : timerStart
    : null;
  const effectiveNow = phase === "paused" && pausedAt ? pausedAt * 1000 : now;
  // Prefer the real current-round timestamp; legacy snapshots describe the completed round.
  const elapsed =
    phase === "unknown" ||
    phase === "waiting" ||
    phase === "finished" ||
    startMs === null ||
    roundTime === null ||
    !displayRound
      ? null
      : Math.max(
          0,
          Math.floor(
            (effectiveNow - startMs) / 1000 -
              (currentRoundStart ? 0 : (roundTime ?? 0)) -
              pausedSeconds,
          ),
        );
  const timerSeconds =
    elapsed === null || !roundTime
      ? null
      : roundTimerSeconds(elapsed, roundTime);
  const progress =
    timerSeconds === null || !roundTime || !displayRound
      ? 0
      : (timerSeconds / roundTime) * 100;
  const time =
    timerSeconds === null
      ? "-:-"
      : `${String(Math.floor(timerSeconds / 60)).padStart(2, "0")}:${String(timerSeconds % 60).padStart(2, "0")}`;
  const waitingForResults =
    phase === "running" &&
    (roundWaiting || (elapsed !== null && roundTime !== null && elapsed >= roundTime));
  return (
    <section className="round-line" aria-label={t("scoreboard.round.current")}>
      <span className="round-label">{t("scoreboard.round.timeline")}</span>
      <div
        className="round-track"
        role="progressbar"
        aria-label={t("scoreboard.round.time")}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={Math.floor(progress)}
      >
        <span
          className="round-track-progress"
          style={{ transform: `scaleX(${progress / 100})` }}
        />
        {Array.from({ length: 18 }, (_, index) => (
          <i key={index} className="round-tick" />
        ))}
        <span
          className="round-head"
          style={{ transform: `translateX(${progress}cqw)` }}
          aria-hidden="true"
        />
      </div>
      <div className="round-timer">
        <div>
          <strong>{time}</strong>
          <small
            className={
              waitingForResults
                ? "round-waiting-label"
                : undefined
            }
            aria-live="polite"
          >
            {phase === "finished"
              ? t("scoreboard.round.finished")
              : phase === "paused"
                ? t("scoreboard.round.paused")
                : waitingForResults
                  ? t("scoreboard.round.waitingResults")
                  : phase === "unknown"
                    ? t("scoreboard.round.unavailable")
                    : t("scoreboard.round.sinceStart")}
          </small>
        </div>
      </div>
    </section>
  );
}
