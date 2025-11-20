import { LiveScoreboardFrame } from "@/features/view-live-scoreboard/ui/LiveScoreboardFrame";

export function LiveScoreboardWidget() {
  return (
    <div className="flex w-full justify-center">
      <LiveScoreboardFrame />
    </div>
  );
}
