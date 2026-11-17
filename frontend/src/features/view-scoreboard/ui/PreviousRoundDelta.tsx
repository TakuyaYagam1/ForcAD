import { useEffect, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { fetchTeamStates } from "@/entities/team-scoreboard/api";
import { formatScore } from "@/shared/lib/formatScore";
import { requestScoreboardHistory } from "../model/historyRequestQueue";
import {
  calculateRoundDelta,
  roundDeltaForDisplay,
  roundDeltaScopeKey,
  type RoundDeltaScope,
} from "../model/roundDelta";

export function PreviousRoundDelta({ scope }: {
  scope: RoundDeltaScope | null
}) {
  const { t } = useTranslation("public");
  const queryClient = useQueryClient();
  const ref = useRef<HTMLSpanElement>(null);
  const [visible, setVisible] = useState(() => {
    return typeof IntersectionObserver === "undefined";
  });
  useEffect(() => {
    const element = ref.current;
    if (!element || typeof IntersectionObserver === "undefined") {
      return;
    }
    const observer = new IntersectionObserver(([entry]) => {
      setVisible(entry.isIntersecting);
    }, {
      rootMargin: "100px 0px"
    });
    observer.observe(element);
    return () => {
      observer.disconnect();
    };
  }, []);

  const scopeKey = scope ? roundDeltaScopeKey(scope) : null;
  const history = useQuery({
    // Separate from the profile's live history: socket packets must not invalidate a completed round.
    queryKey: ["scoreboard-round-history", scopeKey],
    queryFn: ({ signal }) => {
      return requestScoreboardHistory(async () => {
        const states = await fetchTeamStates(scope!.teamId, signal);
        return {
          scopeKey: scopeKey!,
          delta: calculateRoundDelta({
            scopeKey: scopeKey!,
            states,
          }, scope!),
        };
      }, signal);
    },
    enabled: visible && scope !== null,
    staleTime: Infinity,
    gcTime: 60_000,
    retry: 1,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  });
  useEffect(() => {
    if (visible || !scopeKey) {
      return;
    }
    const queryKey = ["scoreboard-round-history", scopeKey];
    const query = queryClient.getQueryCache().find({
      queryKey,
      exact: true
    });
    // Disabling an observer does not abort its queued request. Leave shared visible consumers running.
    if (query && !query.isActive()) {
      void queryClient.cancelQueries({
        queryKey,
        exact: true
      });
    }
  }, [queryClient, scopeKey, visible]);
  const delta = history.data?.scopeKey === scopeKey ? history.data?.delta : null;
  const value = delta?.status === "ready" ? roundDeltaForDisplay(delta.value) : null;
  const tone = value === null || value === 0 ? "neutral" : value > 0 ? "gain" : "loss";
  const formatted = value === null ? "—" : `${value > 0 ? "+" : value < 0 ? "−" : ""}${formatScore(Math.abs(value))}`;
  const reason = !scope ? "unavailable" : history.isError ? "error" : delta && delta.status !== "ready" ? delta.status : "loading";
  const explanation = value !== null && scope
    ? t("scoreboard.delta.explanation", {
      round: scope.round,
      previous: scope.round - 1,
      value: formatted,
    })
    : t(`scoreboard.delta.${reason}`);
  return (
    <span ref={ref} className={`round-delta round-delta--${tone}`} title={explanation} aria-label={explanation}>
      <span className="round-delta-value" aria-hidden="true">{formatted}</span>
      <span className="round-delta-round" aria-hidden="true">
        {scope ? t("scoreboard.delta.round", {
          round: scope.round
        }) : t("scoreboard.delta.lastRound")}
      </span>
    </span>
  );
}
