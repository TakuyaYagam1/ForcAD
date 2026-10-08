import {
  Timer,
  Flag,
  SquareTerminal,
  Trophy,
  Shield,
  ListChecks,
  UsersRound,
  ChartNoAxesColumnIncreasing,
} from "lucide-react";

const outlineIcons = {
  timer: Timer,
  flag: Flag,
  terminal: SquareTerminal,
  trophy: Trophy,
  shield: Shield,
  check: ListChecks,
  team: UsersRound,
  podium: ChartNoAxesColumnIncreasing,
  cup: Trophy,
};

export type BrandIconName =
  | "timer"
  | "flag"
  | "terminal"
  | "trophy"
  | "shield"
  | "check"
  | "team"
  | "podium"
  | "cup";

export function BrandIcon({
  name,
  className = "",
  compact = false,
  plain = false,
}: {
  name: BrandIconName;
  className?: string;
  compact?: boolean;
  plain?: boolean;
}) {
  const OutlineIcon = outlineIcons[name];
  return (
    <span
      className={`brand-icon ${plain ? "brand-icon--plain" : ""} ${className}`}
      aria-hidden="true"
    >
      {plain && name === "podium" ? (
        <svg
          viewBox="0 0 42 40"
          fill="none"
          stroke="currentColor"
          strokeWidth="1.4"
        >
          <path d="M3 23H15V37H3ZM15 15H27V37H15ZM27 27H39V37H27ZM19 8V12M23 8V12M8 17V20M10 17V20M31 21V24M35 21V24" />
          <circle cx="21" cy="4.5" r="2.5" />
          <path d="M17 8H25M18 23H24M7 29H11M31 32H35" />
        </svg>
      ) : plain ? (
        <OutlineIcon strokeWidth={1.4} />
      ) : (
        <img
          src={`/brand/icons/${name}${compact ? "-small" : ""}.png`}
          alt=""
          loading="lazy"
        />
      )}
    </span>
  );
}
