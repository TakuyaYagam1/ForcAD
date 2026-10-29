import { ArrowRight, ArrowUpRight } from "lucide-react";
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import {
  useLiveScoreboardStore,
  type LiveEvent,
} from "@/entities/live-scoreboard/model/store";
import { useScoreboardStore } from "@/entities/scoreboard/model/store";
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
  const timestamp = new Date(event.ts);
  if (event.kind === "admin_message") {
    return (
      <li className={`event-item event-item--announcement${compact ? " event-item--compact" : ""}`}>
        <time dateTime={timestamp.toISOString()}>
          {timestamp.toLocaleTimeString(getLocale(), {
            hour: "2-digit",
            minute: "2-digit",
            second: compact ? undefined : "2-digit",
          })}
        </time>
        <div className="event-announcement">
          <strong>{event.title}</strong>
          <p>{event.markdown}</p>
          {event.author && <small>{event.author}</small>}
        </div>
      </li>
    );
  }
  const attacker = teams?.find((team) => team.id === event.attackerId);
  const victim = teams?.find((team) => team.id === event.victimId);
  const delta = event.delta;
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
            {delta !== undefined && (
              <span className={`event-gain ${delta < 0 ? "event-gain--negative" : ""}`}>
                {delta >= 0 ? "+" : ""}
                {formatScore(delta)}
              </span>
            )}
          </div>
        </div>
      </li>
    );
  return (
    <li className="event-item event-item--full">
      <time dateTime={timestamp.toISOString()}>
        {timestamp.toLocaleTimeString(getLocale(), {
          hour: "2-digit",
          minute: "2-digit",
          second: "2-digit",
        })}
      </time>
      <p className="event-sentence">
        {t("events.captureLead")}{" "}
        <Link className="event-team" to={`/team/${event.attackerId}`}>
          <span aria-hidden="true"><TeamAvatar name={event.attackerName} src={attacker?.logo_path} size="small" /></span>
          <span className="event-team-name">{event.attackerName}</span>
        </Link>{" "}
        {t("events.capturedFrom")}{" "}
        <Link className="event-team" to={`/team/${event.victimId}`}>
          <span aria-hidden="true"><TeamAvatar name={event.victimName} src={victim?.logo_path} size="small" /></span>
          <span className="event-team-name">{event.victimName}</span>
        </Link>{" "}
        {t("events.onService")}{" "}
        <strong>{event.taskName}</strong>
        {delta !== undefined && (
          <>
            {" "}{t("events.earned")}{" "}
            <span className={delta < 0 ? "event-points event-points--negative" : "event-points"}>
              {delta >= 0 ? "+" : ""}{formatScore(delta)} FP
            </span>
          </>
        )}
      </p>
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
          (event) =>
            event.kind === "admin_message" ||
            event.attackerId === teamId ||
            event.victimId === teamId,
        )
  ).slice(0, 3);
  return (
    <section
      className={`recent-events ${teamId === undefined ? "recent-events--strip" : "recent-events--team team-panel"}`}
    >
      <div className="section-heading">
        <h2>{teamId === undefined ? t("events.latest") : t("events.team")}</h2>
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
