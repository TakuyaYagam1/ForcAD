import { AppShell } from "@/shared/ui/layout/AppShell";
import { LiveScoreboardWidget } from "@/widgets/live-scoreboard/ui/LiveScoreboardWidget";

export function LiveScoreboardPage() {
  return (
    <AppShell>
      <div className="flex flex-col items-center gap-4">
        <LiveScoreboardWidget />
      </div>
    </AppShell>
  );
}
