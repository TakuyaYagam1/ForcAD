import { useState } from "react";
import { Search, WifiOff } from "lucide-react";
import { useLiveScoreboardStore } from "@/entities/live-scoreboard/model/store";
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
  const filtered = events.filter((event) => {
    const text = event.kind === "admin_message"
      ? `${event.title} ${event.markdown} ${event.author ?? ""}`
      : `${event.attackerName} ${event.victimName} ${event.taskName}`;
    return text.toLocaleLowerCase(locale).includes(query);
  });
  return (
    <div className="w-full">
      <h1 className="sr-only">{t("liveScoreboard.title")}</h1>
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
      </div>
      {filtered.length ? (
        <ul className="events-list">
          {filtered.map((event) => (
            <EventItem key={event.id} event={event} />
          ))}
        </ul>
      ) : (
        <div className="empty-state mt-5">
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
