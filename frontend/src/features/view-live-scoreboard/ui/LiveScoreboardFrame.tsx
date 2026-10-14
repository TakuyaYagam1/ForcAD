import { useState } from "react";
import { Search, WifiOff } from "lucide-react";
import { useLiveScoreboardStore } from "@/entities/live-scoreboard/model/store";
import { BrandIcon } from "@/shared/ui/brand/BrandIcon";
import { PageHero } from "@/shared/ui/brand/PageHero";
import { EventItem } from "@/shared/ui/brand/RecentEvents";
import { useTranslation } from "react-i18next";
import { getLocale } from "@/shared/i18n";

export function LiveScoreboardFrame() {
  const { t } = useTranslation("public");
  const events = useLiveScoreboardStore((state) => state.events);
  const error = useLiveScoreboardStore((state) => state.error);
  const [filter, setFilter] = useState("");
  const locale = getLocale();
  const query = filter.trim().toLocaleLowerCase(locale);
  const filtered = events.filter((event) =>
    `${event.attackerName} ${event.victimName} ${event.taskName}`
      .toLocaleLowerCase(locale)
      .includes(query),
  );
  return (
    <div className="w-full">
      <PageHero
        compact
        title={t("liveScoreboard.title")}
        subtitle={t("liveScoreboard.subtitle")}
      >
        <div className="hero-metadata">
          <span>
            <BrandIcon name="activity" />
            {t("liveScoreboard.flagCaptures")}
          </span>
          <span>{t("liveScoreboard.recentCompetitionEvents")}</span>
        </div>
      </PageHero>
      {error && (
        <div className="notice" role="status">
          <WifiOff size={16} />
          {t("liveScoreboard.reconnecting")}
        </div>
      )}
      <div className="events-toolbar">
        <label className="search-field">
          <Search size={17} />
          <input
            type="search"
            aria-label={t("liveScoreboard.searchLabel")}
            value={filter}
            onChange={(event) => setFilter(event.target.value)}
            placeholder={t("liveScoreboard.searchPlaceholder")}
          />
        </label>
        <p className="events-caption">
          {t("liveScoreboard.eventCount", {
            count: events.length,
            formattedCount: new Intl.NumberFormat(locale).format(events.length),
          })}
        </p>
      </div>
      {filtered.length ? (
        <ul className="events-list">
          {filtered.map((event) => (
            <EventItem key={event.id} event={event} />
          ))}
        </ul>
      ) : (
        <div className="empty-state mt-5">
          <BrandIcon name="activity" />
          <h2>
            {query
              ? t("liveScoreboard.notFoundTitle")
              : t("liveScoreboard.quietTitle")}
          </h2>
          <p>
            {query
              ? t("liveScoreboard.notFoundDescription")
              : t("liveScoreboard.quietDescription")}
          </p>
        </div>
      )}
      <p className="table-hint">
        {t("liveScoreboard.historyHint")}
      </p>
    </div>
  );
}
