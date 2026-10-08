const numberFormat = new Intl.NumberFormat("ru-RU", {
  minimumFractionDigits: 0,
  maximumFractionDigits: 2,
});

export function formatScore(value: number) {
  return numberFormat.format(Number.isFinite(value) ? value : 0);
}
