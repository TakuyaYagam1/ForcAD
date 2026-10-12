import { useParams } from "react-router-dom";
import { AppShell } from "@/shared/ui/layout/AppShell";
import { TeamScoreboardWidget } from "@/widgets/team-scoreboard/ui/TeamScoreboardWidget";
import { normalizeUuid } from "@/shared/lib/uuid";

export function TeamScoreboardPage() {
  const { teamId: rawTeamId } = useParams();
  const isInvalid = !normalizeUuid(rawTeamId);

  return (
    <AppShell>
      {isInvalid ? (
        <div className="rounded-xl border border-red-500/40 bg-red-500/10 px-4 py-3 text-sm text-red-100">
          Некорректный идентификатор команды: {rawTeamId}
        </div>
      ) : (
        <TeamScoreboardWidget />
      )}
    </AppShell>
  );
}
