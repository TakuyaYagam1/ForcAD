import { getLocale } from "../i18n/index.ts";

const numberFormatters = new Map<string, Intl.NumberFormat>();

export function formatScore(value: number) {
  const locale = getLocale();
  let numberFormat = numberFormatters.get(locale);

  if (!numberFormat) {
    numberFormat = new Intl.NumberFormat(locale, {
      minimumFractionDigits: 0,
      maximumFractionDigits: 2,
    });
    numberFormatters.set(locale, numberFormat);
  }

  return numberFormat.format(Number.isFinite(value) ? value : 0);
}
