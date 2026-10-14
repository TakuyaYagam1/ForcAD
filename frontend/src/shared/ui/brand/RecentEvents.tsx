import { ArrowRight, ArrowUpRight } from "lucide-react";
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import {
  useLiveScoreboardStore,
  type LiveEvent,
} from "@/entities/live-scoreboard/model/store";
import { useScoreboardStore } from "@/entities/scoreboard/model/store";
import { BrandIcon } from "./BrandIcon";
import { TeamAvatar } from "./TeamAvatar";
import { formatScore } from "@/shared/lib/formatScore";
import { getLocale } from "@/shared/i18n";

export function EventItem({
  event,
  compact = false,
}: {
  event: LiveEvent;
  compact?: boolean;
}) {
  const { t } = useTranslation("common");
  const teams = useScoreboardStore((state) => state.teams);
  const attacker = teams?.find((team) => team.id === event.attackerId);
  const victim = teams?.find((team) => team.id === event.victimId);
  const delta = event.delta;
  const timestamp = new Date(event.ts);
  if (compact)
    return (
      <li className="event-item event-item--compact">
        <time dateTime={timestamp.toISOString()}>
          {timestamp.toLocaleTimeString(getLocale(), {
            hour: "2-digit",
            minute: "2-digit",
          })}
        </time>
        <div className="event-avatars">
          <Link
            to={`/team/${event.attackerId}`}
            aria-label={t("team.linkLabel", { name: event.attackerName })}
          >
            <TeamAvatar
              name={event.attackerName}
              src={attacker?.logo_path}
              size="small"
            />
          </Link>
          <ArrowRight size={18} aria-label={t("events.capturedFrom")} />
          <Link
            to={`/team/${event.victimId}`}
            aria-label={t("team.linkLabel", { name: event.victimName })}
          >
            <TeamAvatar
              name={event.victimName}
              src={victim?.logo_path}
              size="small"
            />
          </Link>
        </div>
        <div className="event-description">
          <div className="event-names">
            <Link to={`/team/${event.attackerId}`}>{event.attackerName}</Link>
            <ArrowRight size={16} />
            <Link to={`/team/${event.victimId}`}>{event.victimName}</Link>
          </div>
          <div className="event-details">
            <span className="event-service">{event.taskName}</span>
            <span
              className={`event-gain ${delta < 0 ? "event-gain--negative" : ""}`}
            >
              {delta >= 0 ? "+" : ""}
              {formatScore(delta)}
            </span>
          </div>
        </div>
      </li>
    );
  return (
    <li className={`event-item ${compact ? "event-item--compact" : ""}`}>
      <time dateTime={new Date(event.ts).toISOString()}>
        {new Date(event.ts).toLocaleTimeString(getLocale(), {
          hour: "2-digit",
          minute: "2-digit",
          second: compact ? undefined : "2-digit",
        })}
      </time>
      <div className="event-match">
        <Link to={`/team/${event.attackerId}`}>
          <TeamAvatar
            name={event.attackerName}
            src={attacker?.logo_path}
            size="small"
          />
          <span>{event.attackerName}</span>
        </Link>
        <ArrowRight aria-label={t("events.capturedFrom")} size={16} />
        <Link to={`/team/${event.victimId}`}>
          <TeamAvatar
            name={event.victimName}
            src={victim?.logo_path}
            size="small"
          />
          <span>{event.victimName}</span>
        </Link>
      </div>
      <span className="event-service">{event.taskName}</span>
      <span className="event-gain">
        {event.delta >= 0 ? "+" : ""}
        {formatScore(event.delta)} <small>{t("events.points")}</small>
      </span>
    </li>
  );
}

export function RecentEvents({ teamId }: { teamId?: string }) {
  const { t } = useTranslation("common");
  const events = useLiveScoreboardStore((state) => state.events);
  const selected = (
    teamId === undefined
      ? events
      : events.filter(
          (event) => event.attackerId === teamId || event.victimId === teamId,
        )
  ).slice(0, 3);
  return (
    <section
      className={`recent-events ${teamId === undefined ? "recent-events--strip" : "recent-events--team team-panel"}`}
    >
      <div className="section-heading">
        <h2>
          <BrandIcon name="activity" plain />
          {teamId === undefined ? t("events.latest") : t("events.team")}
        </h2>
        <Link to="/live" className="text-link">
          {t("events.all")} <ArrowUpRight size={15} />
        </Link>
      </div>
      {selected.length ? (
        <ul className="recent-events-grid">
          {selected.map((event) => (
            <EventItem key={event.id} event={event} compact />
          ))}
        </ul>
      ) : (
        <p className="quiet-empty">
          {t("events.empty")}
        </p>
      )}
    </section>
  );
}
