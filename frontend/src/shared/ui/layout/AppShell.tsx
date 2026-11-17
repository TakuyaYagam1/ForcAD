import { useEffect, useRef, useState, type ReactNode } from "react";
import { Activity, ListOrdered, Users } from "lucide-react";
import { Link, NavLink, useLocation } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { useQuery } from "@tanstack/react-query";
import { fetchScoreboardConfig } from "@/entities/scoreboard/api/config";
import { StatusesBar } from "@/features/view-scoreboard/ui/StatusesBar";
import { useScoreboardStore } from "@/entities/scoreboard/model/store";
import { usePageEntrance } from "@/shared/lib/usePageEntrance";
import { useRouteScroll } from "@/shared/lib/useRouteScroll";
import { gameStatusLabel } from "@/shared/lib/gameStatus";
import { brand } from "@/app/config/brand";
import { LanguageSwitch } from "./LanguageSwitch";

export function AppShell({ children, headerActions, showNavigation = true }: {
  children: ReactNode;
  headerActions?: ReactNode;
  showNavigation?: boolean
}) {
  const { t } = useTranslation("shell");
  const location = useLocation();
  useRouteScroll();
  const keyboardNavigation = useRef(false);
  const contentRef = usePageEntrance(location.pathname, keyboardNavigation);

  useEffect(() => {
    const updateVisibility = () => {
      document.documentElement.dataset.pageVisibility = document.visibilityState;
    };
    updateVisibility();
    document.addEventListener("visibilitychange", updateVisibility);
    return () => {
      document.removeEventListener("visibilitychange", updateVisibility);
      delete document.documentElement.dataset.pageVisibility;
    };
  }, []);

  const round = useScoreboardStore(
    (state) => {
      return state.runtimeRound ?? state.round
    },
  );
  const snapshotRound = useScoreboardStore((state) => {
    return state.round
  });
  const roundStart = useScoreboardStore((state) => {
    return state.roundStart
  });
  const roundTime = useScoreboardStore((state) => {
    return state.roundTime
  });
  const setRoundTime = useScoreboardStore((state) => {
    return state.setRoundTime
  });
  const showRoundProgress = location.pathname !== "/admin/login";
  const config = useQuery({
    queryKey: ["game-config"],
    queryFn: ({ signal }) => {
      return fetchScoreboardConfig(signal)
    },
    enabled: showRoundProgress && roundTime === null,
    retry: 2,
    refetchInterval: 10000,
  });
  useEffect(() => {
    if (config.data?.round_time) {
      setRoundTime(config.data.round_time);
    }
  }, [config.data, setRoundTime]);

  const teams = useScoreboardStore((state) => {
    return state.teams
  });
  const error = useScoreboardStore((state) => {
    return state.error
  });
  const connected = useScoreboardStore((state) => {
    return state.connected
  });
  const phase = useScoreboardStore((state) => {
    return state.phase
  });
  const resultsPending = useScoreboardStore((state) => {
    return state.resultsPending
  });

  return (
    <div
      className="brand-app"
      onPointerDownCapture={() => {
        keyboardNavigation.current = false;
        document.documentElement.dataset.inputModality = "pointer";
      }}
      onKeyDownCapture={() => {
        keyboardNavigation.current = true;
        document.documentElement.dataset.inputModality = "keyboard";
      }}
    >
      <a href="#main-content" className="skip-link">
        {t("skip")}
      </a>

      <header className="site-header">
        <div className="header-inner">
          <Link
            to="/"
            className="brand-lockup"
            aria-label={t("home", {
              brand: brand.name
            })}
          >
            <BrandLogo />
            <span>{brand.name}</span>
          </Link>

          {showRoundProgress && <StatusesBar round={snapshotRound} roundStart={roundStart} />}

          {showNavigation && (
            <nav className="main-nav" aria-label={t("navigation")}>
              <NavLink to="/live"><Activity aria-hidden="true" />{t("events")}</NavLink>
              <NavLink to="/" end><ListOrdered aria-hidden="true" />{t("scoreboard")}</NavLink>
              <NavLink to="/teams"><Users aria-hidden="true" />{t("teams")}</NavLink>
            </nav>
          )}

          <div className={`header-tools${showNavigation ? "" : " header-tools--admin"}`}>
            <div className="header-status">
              {teams && !error && round > 0 && (
                <span className="header-round">{t("round", {
                  round
                })}</span>
              )}
              <span
                className={`connection-dot ${error || (!connected && teams) ? "connection-dot--error" : teams ? "connection-dot--ready" : ""}`}
              />
              <span>
                {error || (!connected && teams)
                  ? t("disconnected")
                  : teams
                    ? gameStatusLabel(phase)
                    : t("connecting")}
              </span>
            </div>
            {headerActions}
            <LanguageSwitch />
          </div>
        </div>
      </header>

      <main id="main-content" className={`site-main${location.pathname === "/" ? " site-main--scoreboard" : ""}${location.pathname.startsWith("/admin/") && location.pathname !== "/admin/login" ? " site-main--admin" : ""}`}>
        {phase === "paused" && (
          <div
            className="notice notice--paused"
            role="status"
          >
            {t("paused")}
          </div>
        )}
        {phase === "finished" && (
          <div className="notice mx-auto my-3 max-w-screen-xl" role="status">
            {t("finished")}{" "}
            {resultsPending
              ? t("pending")
              : t("stopped")}
          </div>
        )}
        <div ref={contentRef} className="page-content">{children}</div>
      </main>
    </div>
  );
}

function BrandLogo() {
  const [imageFailed, setImageFailed] = useState(false);
  if (imageFailed) {
    return (
      <svg className="brand-mark-fallback" viewBox="0 0 48 48" fill="none" aria-hidden="true" focusable="false">
        <rect x="4" y="4" width="40" height="40" rx="11" stroke="var(--brand-primary)" strokeOpacity=".42" />
        <path d="M17 34V14h16M17 23h12" stroke="var(--brand-primary)" strokeWidth="3.5" strokeLinecap="round" strokeLinejoin="round" />
        <path d="M31 29v5" stroke="var(--brand-accent)" strokeWidth="3.5" strokeLinecap="round" />
      </svg>
    );
  }
  return <img src={brand.logo} alt="" onError={() => {
    return setImageFailed(true)
  }} />;
}
