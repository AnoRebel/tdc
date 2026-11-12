/*
 * Money, as the server keeps it and as people read and type it.
 *
 * The server stores whole minor units and a currency code, and never a
 * fraction: 1 500 000 TZS is 150000000, and 12.50 USD is 1250. What a minor
 * unit is depends on the currency — most have two decimal places, some none
 * and a few three — so that comes from the platform's own table of currencies
 * rather than from a guess here.
 *
 * Reading what somebody typed is done on the digits as text, never through a
 * floating-point number, so 0.29 does not come back as 28 minor units.
 */

/** How many decimal places one currency has; two when the code is unknown. */
export function minorDigits(currency: string): number {
  try {
    return (
      new Intl.NumberFormat("en", { style: "currency", currency }).resolvedOptions()
        .maximumFractionDigits ?? 2
    );
  } catch {
    return 2;
  }
}

/**
 * An amount in minor units, written for reading, with its currency.
 *
 * In one fixed format unless told otherwise, for the reason dates.ts gives:
 * the runtime's idea of a locale differs between the browser and the desktop
 * webview, and two people looking at the same price should see it written the
 * same way.
 */
export function formatMoney(minor: number, currency: string, locale = "en"): string {
  const digits = minorDigits(currency);
  const value = minor / 10 ** digits;
  try {
    return new Intl.NumberFormat(locale, {
      style: "currency",
      currency,
      currencyDisplay: "code",
      minimumFractionDigits: digits,
      maximumFractionDigits: digits,
    }).format(value);
  } catch {
    return `${currency} ${value.toFixed(digits)}`;
  }
}

/** An amount in minor units, written as somebody would type it: 1500.50. */
export function amountText(minor: number, currency: string): string {
  const digits = minorDigits(currency);
  if (digits === 0) return String(minor);
  const text = String(minor).padStart(digits + 1, "0");
  return `${text.slice(0, -digits)}.${text.slice(-digits)}`;
}

/**
 * What somebody typed, in minor units — or null when it is not an amount in
 * that currency: negative, more decimal places than the currency has, or not
 * a number at all. Spaces, commas and underscores between digits are taken
 * as grouping, since people write 1,500,000 and 1 500 000 alike.
 */
export function parseAmount(text: string, currency: string): number | null {
  const cleaned = text.trim().replaceAll(/[\s,_]/g, "");
  const match = /^(\d+)(?:\.(\d*))?$/.exec(cleaned);
  if (!match) return null;
  const digits = minorDigits(currency);
  const whole = match[1] ?? "0";
  const fraction = match[2] ?? "";
  if (fraction.length > digits) return null;
  const minor = Number(whole + fraction.padEnd(digits, "0"));
  return Number.isSafeInteger(minor) ? minor : null;
}
