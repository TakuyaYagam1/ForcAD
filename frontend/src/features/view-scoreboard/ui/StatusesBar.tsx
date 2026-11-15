import { useScoreboardStore } from "@/entities/scoreboard/model/store";
import { useTranslation } from "react-i18next";
import { useEffect, useState } from "react";
import { formatRoundCountdown, resolveRoundTimer } from "../model/roundTimer";
import { useAsciiRoundTrack } from "../model/useAsciiRoundTrack";
import "./StatusesBar.css";

export function StatusesBar({ round, roundStart }: {
  round?: number;
  roundStart?: number | null
}) {
  const { t } = useTranslation("public");
  const roundTime = useScoreboardStore((state) => {
    return state.roundTime
  });
  const runtimeRound = useScoreboardStore((state) => {
    return state.runtimeRound
  });
  const currentRoundStart = useScoreboardStore((state) => {
    return state.currentRoundStart
  });
  const phase = useScoreboardStore((state) => {
    return state.phase
  });
  const roundWaiting = useScoreboardStore((state) => {
    return state.roundWaiting
  });
  const pausedAt = useScoreboardStore((state) => {
    return state.pausedAt
  });
  const pausedSeconds = useScoreboardStore((state) => {
    return state.pausedSeconds
  });
  const [now, setNow] = useState(() => {
    return Date.now()
  });
  useEffect(() => {
    const timer = window.setInterval(() => {
      return setNow(Date.now())
    }, 250);
    return () => {
      return window.clearInterval(timer)
    };
  }, []);

  const timer = resolveRoundTimer({
    nowMs: now,
    phase,
    roundTime,
    currentRoundStart,
    legacyRoundStart: roundStart,
    round: runtimeRound ?? round,
    pausedAt,
    pausedSeconds,
    roundWaiting,
  });
  const percent = timer.progress === null ? null : Math.floor(timer.progress);
  const running = phase === "running" && timer.progress !== null && !timer.waitingForResults;
  const { trackRef, columns: measuredColumns } = useAsciiRoundTrack(running);
  const columns = measuredColumns;
  const filledColumns = Math.floor((timer.progress ?? 0) / 100 * columns);
  const countdown = formatRoundCountdown(timer.remainingSeconds);
  const status = phase === "finished"
    ? t("scoreboard.round.finished")
    : phase === "waiting"
      ? t("scoreboard.round.waitingStart")
      : timer.progress === null
        ? t("scoreboard.round.unavailable")
        : phase === "paused"
          ? t("scoreboard.round.paused")
          : timer.waitingForResults
            ? t("scoreboard.round.waitingResults")
            : t("scoreboard.round.remaining");
  const valueText = percent === null
    ? status
    : t("scoreboard.round.progressDescription", {
      percent,
      time: countdown,
      status
    });

  return (
    <section className="round-line" aria-label={t("scoreboard.round.current")}>
      <div
        ref={trackRef}
        className="round-track"
        style={{
          gridTemplateColumns: `repeat(${columns + 2}, minmax(0, 1fr))`
        }}
        role="progressbar"
        aria-label={t("scoreboard.round.time")}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={percent ?? undefined}
        aria-valuetext={valueText}
      >
        <span className="round-bracket" aria-hidden="true">[</span>
        {Array.from({
          length: columns
        }, (_, index) => {
          const filled = index < filledColumns;
          const tip = timer.progress !== null && index === filledColumns;
          return (
            <span
              key={index}
              className={`round-character${filled ? " round-character--filled" : ""}${tip ? " round-character--tip" : ""}`}
              aria-hidden="true"
            >
              {tip ? (
                <>
                  <span className="round-cursor-rest">{">"}</span>
                  <span className="round-cursor-frames">
                    {["/", "-", "\\", "|"].map((character, frame) => {
                      return <span key={frame} style={{
                        animationDelay: `${frame * 250}ms`
                      }}>{character}</span>
                    })}
                  </span>
                </>
              ) : filled ? "=" : "-"}
            </span>
          );
        })}
        <span className="round-bracket" aria-hidden="true">]</span>
      </div>
      <div className="round-timer">
        <strong>{countdown}</strong>
        <small className="sr-only" aria-live="polite">{status}</small>
      </div>
    </section>
  );
}
