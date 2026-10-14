import { useState } from "react";
import { ArrowUpDown, Flag, RotateCcw } from "lucide-react";
import { prepareDemoGame, resetDemoData, simulateDemoCapture } from "./install";
import { leaveDemoMode } from "./mode";
import { useTranslation } from "react-i18next";

export function DemoControls({ onDataChange }: { onDataChange: () => void }) {
  const { t } = useTranslation("shell");
  const [message, setMessage] = useState<"prepared" | "reordered" | "captured" | "restored" | null>(null);
  function run(action: () => void, feedback: NonNullable<typeof message>) {
    action();
    onDataChange();
    setMessage(feedback);
  }
  return (
    <aside className="demo-panel" aria-label={t("demo.controls")}>
      <div className="demo-panel-inner">
        <div className="demo-panel-label">
          <strong>{t("demo.title")}</strong>
          <span>{t("demo.description")}</span>
        </div>
        <div className="demo-panel-actions">
          <button type="button" onClick={() => run(prepareDemoGame, "prepared")}>
            {t("demo.prepare")}
          </button>
          <button
            type="button"
            onClick={() =>
              run(() => simulateDemoCapture(true), "reordered")
            }
          >
            <ArrowUpDown size={14} />
            {t("demo.reorder")}
          </button>
          <button
            type="button"
            onClick={() =>
              run(() => simulateDemoCapture(), "captured")
            }
          >
            <Flag size={14} />
            {t("demo.capture")}
          </button>
          <button
            type="button"
            onClick={() => run(resetDemoData, "restored")}
          >
            <RotateCcw size={14} />
            {t("demo.reset")}
          </button>
          <button
            type="button"
            className="demo-panel-exit"
            onClick={leaveDemoMode}
          >
            {t("demo.exit")}
          </button>
        </div>
        <span className="sr-only" role="status" aria-live="polite">
          {message && t(`demo.${message}`)}
        </span>
      </div>
    </aside>
  );
}
