// src/pages/scoreboard/ui/ScoreboardPage.tsx
import { AppShell } from "@/shared/ui/layout/AppShell";
import { ScoreboardWidget } from "@/widgets/scoreboard/ui/ScoreboardWidget";

export function ScoreboardPage() {
  return (
    <AppShell>
      <ScoreboardWidget />
    </AppShell>
  );
}
