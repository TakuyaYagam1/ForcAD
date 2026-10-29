export type CheckerStatus = "waiting" | "reporting" | "failed" | "stalled";

export function checkerStatus(
  currentRound: number,
  reported: number,
  failed: number,
  running: boolean,
): CheckerStatus {
  if (running && currentRound >= 2 && reported === 0) {
    return "stalled";
  }
  if (failed > 0) return "failed";
  if (reported === 0) return "waiting";
  return "reporting";
}
