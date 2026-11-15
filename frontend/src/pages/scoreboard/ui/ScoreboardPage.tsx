// src/pages/scoreboard/ui/ScoreboardPage.tsx
import { useScoreboardStore } from "@/entities/scoreboard/model/store";
import { StatusesBar } from "@/features/view-scoreboard/ui/StatusesBar";
import { AppShell } from "@/shared/ui/layout/AppShell";
import { ScoreboardWidget } from "@/widgets/scoreboard/ui/ScoreboardWidget";

export function ScoreboardPage() {
  const round = useScoreboardStore((state) => {
    return state.round
  });
  const roundStart = useScoreboardStore((state) => {
    return state.roundStart
  });
  return (
    <AppShell headerProgress={<StatusesBar round={round} roundStart={roundStart} />}>
      <ScoreboardWidget />
    </AppShell>
  );
}
