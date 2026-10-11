export interface SlaCheckCount {
  checks: number;
  checksPassed: number;
}

export function getSlaPercent(
  checks: number,
  checksPassed: number,
): number | null {
  const denominator = Number.isSafeInteger(checks) ? Math.max(0, checks) : 0;
  if (denominator === 0) return null;

  const passed = Number.isSafeInteger(checksPassed)
    ? Math.min(denominator, Math.max(0, checksPassed))
    : 0;
  return (100 * passed) / denominator;
}

export function getCombinedSlaPercent(
  cells: readonly SlaCheckCount[],
): number | null {
  let checks = 0;
  let checksPassed = 0;

  for (const cell of cells) {
    if (!Number.isSafeInteger(cell.checks) || cell.checks <= 0) continue;
    checks += cell.checks;
    if (Number.isSafeInteger(cell.checksPassed)) {
      checksPassed += Math.min(cell.checks, Math.max(0, cell.checksPassed));
    }
  }

  return getSlaPercent(checks, checksPassed);
}
