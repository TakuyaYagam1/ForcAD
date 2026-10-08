import { useEffect, useState } from "react";
import { useScoreboardStore } from "@/entities/scoreboard/model/store";
import { BrandIcon } from "@/shared/ui/brand/BrandIcon";

export function StatusesBar({
  round,
  roundStart,
}: {
  round?: number;
  roundStart?: number | null;
}) {
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
    phase === "unknown" || phase === "waiting" || phase === "finished" ||
    startMs === null || roundTime === null || !displayRound
      ? null
      : Math.max(
          0,
          Math.floor(
            (effectiveNow - startMs) / 1000 -
              (currentRoundStart ? 0 : (roundTime ?? 0)) -
              pausedSeconds,
          ),
        );
  const progress =
    elapsed === null || !roundTime || !displayRound
      ? 0
      : Math.min(100, (elapsed / roundTime) * 100);
  const time =
    elapsed === null
      ? "—:—"
      : `${String(Math.floor(elapsed / 60)).padStart(2, "0")}:${String(elapsed % 60).padStart(2, "0")}`;
  return (
    <section className="round-line" aria-label="Текущий раунд">
      <span className="round-label">Линия раундов</span>
      <div
        className="round-track"
        role="progressbar"
        aria-label="Время текущего раунда"
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={Math.floor(progress)}
      >
        <span
          className="round-track-progress"
          style={{ width: `${progress}%` }}
        />
        {Array.from({ length: 18 }, (_, index) => (
          <i key={index} className="round-tick" />
        ))}
        <span
          className="round-head"
          style={{ left: `calc(${progress}% - 7px)` }}
        >
          <b className="round-current">Раунд {displayRound || "—"}</b>
        </span>
      </div>
      <div className="round-timer">
        <BrandIcon name="timer" plain />
        <div>
          <strong>{time}</strong>
          <small
            className={
              phase === "running" && roundWaiting
                ? "round-waiting-label"
                : undefined
            }
            aria-live="polite"
          >
            {phase === "finished"
              ? "Игра завершилась"
              : phase === "paused"
              ? "Пауза"
              : phase === "running" && roundWaiting
                ? "Ожидание результатов проверок"
                : phase === "unknown"
                  ? "Статус недоступен"
                  : "С начала раунда"}
          </small>
        </div>
      </div>
    </section>
  );
}
