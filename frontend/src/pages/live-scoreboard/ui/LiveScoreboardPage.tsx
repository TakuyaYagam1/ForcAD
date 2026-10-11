import { AppShell } from "@/shared/ui/layout/AppShell";
import { LiveScoreboardWidget } from "@/widgets/live-scoreboard/ui/LiveScoreboardWidget";

export function LiveScoreboardPage() {
  return (
    <AppShell>
      <LiveScoreboardWidget />
    </AppShell>
  );
}
