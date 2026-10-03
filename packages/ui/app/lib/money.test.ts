import { describe, expect, it } from "vitest";

import { amountText, formatMoney, minorDigits, parseAmount } from "./money";

/*
 * Prices in a proposal are what two people agree to, so a rounding slip here
 * would put a different number in front of each of them than the one sent.
 */

describe("minor units", () => {
  it("come from the currency", () => {
    expect(minorDigits("USD")).toBe(2);
    expect(minorDigits("JPY")).toBe(0);
    expect(minorDigits("KWD")).toBe(3);
  });

  it("default to two for a code the platform does not know", () => {
    expect(minorDigits("ZZZ")).toBe(2);
    expect(minorDigits("not a code")).toBe(2);
  });
});

describe("reading what somebody typed", () => {
  it("turns an amount into minor units without floating-point error", () => {
    expect(parseAmount("0.29", "USD")).toBe(29);
    expect(parseAmount("12.5", "USD")).toBe(1250);
    expect(parseAmount("1,500,000", "TZS")).toBe(150_000_000);
    expect(parseAmount("1 500 000.00", "TZS")).toBe(150_000_000);
    expect(parseAmount("300", "JPY")).toBe(300);
  });

  it("refuses what is not an amount in that currency", () => {
    expect(parseAmount("", "USD")).toBeNull();
    expect(parseAmount("-5", "USD")).toBeNull();
    expect(parseAmount("1.234", "USD")).toBeNull();
    expect(parseAmount("1.5", "JPY")).toBeNull();
    expect(parseAmount("ten", "USD")).toBeNull();
    expect(parseAmount("1e5", "USD")).toBeNull();
  });

  it("comes back to the same text it was written as", () => {
    for (const [text, currency] of [
      ["0.29", "USD"],
      ["1500.50", "TZS"],
      ["300", "JPY"],
      ["0.005", "KWD"],
    ] as const) {
      const minor = parseAmount(text, currency);
      expect(minor).not.toBeNull();
      expect(amountText(minor ?? 0, currency)).toBe(text);
    }
  });
});

describe("showing an amount", () => {
  it("shows the currency and every minor digit", () => {
    expect(formatMoney(150_000_000, "TZS", "en")).toMatch(/TZS\s1,500,000\.00/);
    expect(formatMoney(29, "USD", "en")).toMatch(/USD\s0\.29/);
    expect(formatMoney(300, "JPY", "en")).toMatch(/JPY\s300$/);
  });
});
