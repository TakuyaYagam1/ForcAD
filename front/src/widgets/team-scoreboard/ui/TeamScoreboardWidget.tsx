import { TeamScoreboardView } from "@/features/view-team-scoreboard/ui/TeamScoreboardView";

interface TeamScoreboardWidgetProps {
  teamId: number;
}

export function TeamScoreboardWidget({ teamId }: TeamScoreboardWidgetProps) {
  return <TeamScoreboardView teamId={teamId} />;
}
