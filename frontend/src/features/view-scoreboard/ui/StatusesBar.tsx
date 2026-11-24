import { useScoreboardStore } from "@/entities/scoreboard/model/store";
import { useTranslation } from "react-i18next";
import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { roundTimerSeconds } from "../model/roundTimer";

function activeRoundElapsedMilliseconds(
  atMs: number,
  startMs: number,
  roundTime: number,
  currentRoundStart: number | null,
  pausedSeconds: number,
): number {
  const legacyRoundOffsetMs = currentRoundStart ? 0 : roundTime * 1000;
  return Math.max(
    0,
    atMs - startMs - legacyRoundOffsetMs - pausedSeconds * 1000,
  );
}

export function StatusesBar({
  round,
  roundStart,
}: {
  round?: number;
  roundStart?: number | null;
}) {
  const { t } = useTranslation("public");
  const roundTime = useScoreboardStore((state) => {
    return state.roundTime
  });
  const runtimeRound = useScoreboardStore((state) => {
    return state.runtimeRound
  });
  const currentRoundStart = useScoreboardStore(
    (state) => {
      return state.currentRoundStart
    },
  );
  const displayRound = runtimeRound ?? round;
  const timerStart = currentRoundStart ?? roundStart;
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
  const progressRef = useRef<HTMLSpanElement>(null);
  const headRef = useRef<HTMLSpanElement>(null);
  const tickReducedMotion = useRef<(() => void) | null>(null);
  useEffect(() => {
    const timer = window.setInterval(() => {
      setNow(Date.now());
      tickReducedMotion.current?.();
    }, 1000);
    return () => {
      window.clearInterval(timer);
    };
  }, []);
  const startMs = timerStart
    ? timerStart < 10_000_000_000
      ? timerStart * 1000
      : timerStart
    : null;
  const effectiveNow = phase === "paused" && pausedAt ? pausedAt * 1000 : now;
  const elapsedMilliseconds =
    phase === "unknown" ||
    phase === "waiting" ||
    phase === "finished" ||
    startMs === null ||
    roundTime === null ||
    !displayRound
      ? null
      : activeRoundElapsedMilliseconds(
        effectiveNow,
        startMs,
        roundTime,
        currentRoundStart,
        pausedSeconds,
      );
  const elapsed =
    elapsedMilliseconds === null
      ? null
      : Math.floor(elapsedMilliseconds / 1000);
  const timerSeconds =
    elapsed === null || !roundTime
      ? null
      : roundTimerSeconds(elapsed, roundTime);
  const progress =
    elapsedMilliseconds === null || !roundTime || !displayRound
      ? 0
      : (roundTimerSeconds(elapsedMilliseconds / 1000, roundTime) / roundTime) * 100;
  const time =
    timerSeconds === null
      ? "-:-"
      : `${String(Math.floor(timerSeconds / 60)).padStart(2, "0")}:${String(timerSeconds % 60).padStart(2, "0")}`;
  const waitingForResults =
    phase === "running" &&
    (roundWaiting || (elapsed !== null && roundTime !== null && elapsed >= roundTime));
  useLayoutEffect(() => {
    const progressElement = progressRef.current;
    const headElement = headRef.current;
    if (
      !progressElement ||
      !headElement ||
      startMs === null ||
      roundTime === null ||
      roundTime <= 0 ||
      !displayRound ||
      (phase !== "running" && phase !== "paused")
    ) {
      return;
    }

    const durationMs = roundTime * 1000;
    const timing: KeyframeAnimationOptions = {
      duration: durationMs,
      iterations: Infinity,
      easing: "linear",
    };
    const animations = [
      progressElement.animate([
        {
          transform: "translateY(-50%) scaleX(0)",
        },
        {
          transform: "translateY(-50%) scaleX(1)",
        },
      ], timing),
      headElement.animate([
        {
          translate: "0px 0px",
        },
        {
          translate: "100cqw 0px",
        },
      ], timing),
    ];

    const motionPreference = window.matchMedia("(prefers-reduced-motion: reduce)");
    const synchronize = () => {
      const atMs = phase === "paused" && pausedAt ? pausedAt * 1000 : Date.now();
      const cyclePositionMs =
        activeRoundElapsedMilliseconds(
          atMs,
          startMs,
          roundTime,
          currentRoundStart,
          pausedSeconds,
        ) % durationMs;
      for (const animation of animations) {
        animation.currentTime = cyclePositionMs;
        if (phase === "paused" || motionPreference.matches) {
          animation.pause();
        } else {
          animation.play();
        }
      }
    };
    tickReducedMotion.current = () => {
      if (phase === "running" && motionPreference.matches) {
        synchronize();
      }
    };
    const handleVisibilityChange = () => {
      if (document.visibilityState === "visible") {
        synchronize();
      }
    };

    synchronize();
    document.addEventListener("visibilitychange", handleVisibilityChange);
    motionPreference.addEventListener("change", synchronize);
    return () => {
      document.removeEventListener("visibilitychange", handleVisibilityChange);
      motionPreference.removeEventListener("change", synchronize);
      for (const animation of animations) {
        animation.cancel();
      }
      tickReducedMotion.current = null;
    };
  }, [
    currentRoundStart,
    displayRound,
    pausedAt,
    pausedSeconds,
    phase,
    roundTime,
    startMs,
  ]);
  return (
    <section className="round-line" aria-label={t("scoreboard.round.current")}>
      <div
        className="round-track"
        role="progressbar"
        aria-label={t("scoreboard.round.time")}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={timerSeconds === null ? undefined : Math.floor(progress)}
      >
        {Array.from({
          length: 18
        }, (_, index) => {
          return (
            <i key={index} className="round-tick" />
          )
        })}
        {timerSeconds !== null && (
          <>
            <span
              ref={progressRef}
              className="round-track-progress"
              aria-hidden="true"
            />
            <span
              ref={headRef}
              className="round-head"
              aria-hidden="true"
            />
          </>
        )}
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
