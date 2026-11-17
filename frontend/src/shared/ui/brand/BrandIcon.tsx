import {
  Timer,
  Activity,
  SquareTerminal,
  ListChecks,
  UsersRound,
  ChartNoAxesColumnIncreasing,
} from "lucide-react";

const outlineIcons = {
  timer: Timer,
  activity: Activity,
  terminal: SquareTerminal,
  check: ListChecks,
  team: UsersRound,
  podium: ChartNoAxesColumnIncreasing,
  score: ChartNoAxesColumnIncreasing,
};

export type BrandIconName =
  | "timer"
  | "activity"
  | "terminal"
  | "check"
  | "team"
  | "podium"
  | "score";

export function BrandIcon({
  name,
  className = "",
  plain = false,
}: {
  name: BrandIconName;
  className?: string;
  plain?: boolean;
}) {
  const OutlineIcon = outlineIcons[name];
  return (
    <span
      className={`brand-icon ${plain ? "brand-icon--plain" : ""} ${className}`}
      aria-hidden="true"
    >
      <OutlineIcon strokeWidth={plain ? 1.5 : 1.7} />
    </span>
  );
}
