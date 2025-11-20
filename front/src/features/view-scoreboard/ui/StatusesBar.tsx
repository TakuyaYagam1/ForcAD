// src/features/view-scoreboard/ui/StatusesBar.tsx
import { useEffect, useState } from "react";
import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";
import { SCOREBOARD_STATUSES } from "@/shared/config/statuses";
import { useScoreboardStore } from "@/entities/scoreboard/model/store";

interface StatusesBarProps {
  round?: number;
  roundStart?: number | null; // unix timestamp (sec) или ms — см. ниже
}

function formatElapsed(from: number | null): string | null {
  if (!from) return null;

  // если бэк шлёт секунды — умножаем на 1000
  const startMs = from < 10_000_000_000 ? from * 1000 : from;
  const diff = Date.now() - startMs;
  if (diff < 0) return "00:00";

  const mins = Math.floor(diff / 60_000);
  const secs = Math.floor((diff % 60_000) / 1000);
  return `${String(mins).padStart(2, "0")}:${String(secs).padStart(2, "0")}`;
}

export function StatusesBar({ round, roundStart }: StatusesBarProps) {
  const [elapsed, setElapsed] = useState<string | null>(null);

  useEffect(() => {
    if (!roundStart) {
      setElapsed(null);
      return;
    }

    const update = () => {
      setElapsed(formatElapsed(roundStart));
    };

    update();
    const id = window.setInterval(update, 1000);

    return () => window.clearInterval(id);
  }, [roundStart]);

  const roundTime = useScoreboardStore((s) => s.roundTime);
  const [progress, setProgress] = useState<number | null>(null);

  useEffect(() => {
    if (!roundStart || !roundTime) {
      setProgress(null);
      return;
    }

    const update = () => {
      const now = Date.now() / 1000;
      const elapsed = now - roundStart;
      let p = elapsed / roundTime;
      p = Math.max(0, Math.min(p, 1));
      setProgress(Math.floor(p * 100));
    };

    update();
    const id = window.setInterval(update, 1000);
    return () => window.clearInterval(id);
  }, [roundStart, roundTime]);

  return (
    <div className="flex flex-col gap-3 rounded-2xl border border-slate-800/80 bg-slate-950/80 px-4 py-3 shadow-lg shadow-indigo-900/40 backdrop-blur">
      {/* Верхняя строка: информация о раунде */}
      <div className="flex flex-col justify-between gap-2 text-sm text-slate-300 md:flex-row md:items-center">
        <div className="flex flex-wrap items-baseline gap-2">
          <span className="text-xs uppercase tracking-[0.3em] text-slate-500">
            Round
          </span>
          <span className="text-lg font-semibold text-slate-100">
            {round ?? "—"}
          </span>
          {elapsed && (
            <span className="text-xs text-slate-400">
              elapsed:{" "}
              <span className="font-mono text-slate-200">{elapsed}</span>
            </span>
          )}
        </div>

        {progress !== null && (
          <div className="flex items-center gap-2 text-xs text-slate-300">
            <span>Round progress</span>
            <div className="h-1 w-32 overflow-hidden rounded-full bg-slate-800">
              <div
                className="h-full bg-emerald-400"
                style={{ width: `${progress}%` }}
              />
            </div>
            <span className="font-mono text-slate-200">{progress}%</span>
          </div>
        )}
      </div>

      {/* Легенда статусов */}
      <div className="flex flex-wrap items-center gap-2">
        <span className="text-xs uppercase tracking-[0.25em] text-slate-500">
          Status legend
        </span>

        <div className="flex flex-wrap gap-1.5">
          {SCOREBOARD_STATUSES.map((s) => (
            <Badge
              key={s.code}
              variant="outline"
              className={cn(
                "border px-2 py-0.5 text-[11px] font-medium uppercase tracking-wide",
                s.badgeClassName
              )}
            >
              {s.label}
            </Badge>
          ))}
        </div>
      </div>
    </div>
  );
}
