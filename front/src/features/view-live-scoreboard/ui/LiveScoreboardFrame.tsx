import { useLiveScoreboardStore } from "@/entities/live-scoreboard/model/store";
import { Card } from "@/components/ui/card";
import { ScrollArea } from "@/components/ui/scroll-area";

export function LiveScoreboardFrame() {
  const events = useLiveScoreboardStore((s) => s.events);
  const error = useLiveScoreboardStore((s) => s.error);

  return (
    <Card className="w-full max-w-3xl border-slate-800 bg-slate-950/80 backdrop-blur shadow-xl shadow-fuchsia-900/40">
      <div className="flex items-center justify-between border-b border-slate-800 px-4 py-3">
        <div className="text-xs uppercase tracking-[0.3em] text-slate-400">
          Live feed
        </div>
        <div className="flex items-center gap-1 text-xs text-slate-400">
          <span className="h-2 w-2 rounded-full bg-emerald-400 animate-pulse" />
          streaming
        </div>
      </div>

      {error && (
        <div className="border-b border-red-500/40 bg-red-500/10 px-4 py-2 text-xs text-red-200">
          {error}
        </div>
      )}

      <ScrollArea className="h-[60vh] px-4 py-3">
        {events.length === 0 ? (
          <div className="text-xs text-slate-500">
            Пока нет краж флагов в этой сессии.
          </div>
        ) : (
          <ul className="space-y-2">
            {events.map((ev) => (
              <li
                key={ev.id}
                className="rounded-lg border border-slate-800/70 bg-slate-900/70 px-3 py-2 text-xs text-slate-100 shadow-sm shadow-fuchsia-900/40"
              >
                <div className="flex flex-wrap gap-1 leading-relaxed">
                  <span className="font-semibold text-fuchsia-300">
                    {ev.attackerName}
                  </span>
                  <span className="text-slate-400">stole a flag from</span>
                  <span className="font-semibold text-sky-300">
                    {ev.victimName}
                  </span>
                  <span className="text-slate-400">on</span>
                  <span className="font-semibold text-emerald-300">
                    {ev.taskName}
                  </span>
                  <span className="text-slate-400">and gained</span>
                  <span className="font-mono text-amber-300">
                    {ev.delta} pts
                  </span>
                </div>
                <div className="mt-1 text-[10px] text-slate-500">
                  {new Date(ev.ts).toLocaleTimeString()}
                </div>
              </li>
            ))}
          </ul>
        )}
      </ScrollArea>
    </Card>
  );
}
