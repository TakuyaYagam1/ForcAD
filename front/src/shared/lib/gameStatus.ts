export type GamePhase = "waiting" | "running" | "paused" | "finished" | "unknown";

export function gameStatusLabel(phase: GamePhase): string {
  switch (phase) {
    case "running": return "Игра идет";
    case "paused": return "Игра приостановлена";
    case "finished": return "Игра завершилась";
    case "waiting": return "Ожидание старта";
    default: return "Статус игры недоступен";
  }
}
