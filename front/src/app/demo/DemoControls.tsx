import { useState } from "react";
import { ArrowUpDown, Flag, RotateCcw } from "lucide-react";
import { resetDemoData, simulateDemoCapture } from "./install";
import { leaveDemoMode } from "./mode";

export function DemoControls({ onDataChange }: { onDataChange: () => void }) {
  const [message, setMessage] = useState("");
  function run(action: () => void, feedback: string) {
    action();
    onDataChange();
    setMessage(feedback);
  }
  return (
    <aside className="demo-panel" aria-label="Управление демонстрацией">
      <div className="demo-panel-inner">
        <div className="demo-panel-label">
          <strong>Демо</strong>
          <span>Данные для примера · Админка: demo / demo</span>
        </div>
        <div className="demo-panel-actions">
          <button
            type="button"
            onClick={() =>
              run(() => simulateDemoCapture(true), "Места команд изменены")
            }
          >
            <ArrowUpDown size={14} />
            Сменить места
          </button>
          <button
            type="button"
            onClick={() =>
              run(() => simulateDemoCapture(), "Добавлен захват флага")
            }
          >
            <Flag size={14} />
            Новое событие
          </button>
          <button
            type="button"
            onClick={() => run(resetDemoData, "Исходные данные восстановлены")}
          >
            <RotateCcw size={14} />
            Сбросить
          </button>
          <button
            type="button"
            className="demo-panel-exit"
            onClick={leaveDemoMode}
          >
            К реальным данным
          </button>
        </div>
        <span className="sr-only" role="status" aria-live="polite">
          {message}
        </span>
      </div>
    </aside>
  );
}
