export function roundTimerSeconds(elapsed: number, roundTime: number): number {
  return elapsed % roundTime;
}
