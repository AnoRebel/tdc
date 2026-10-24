import { describe, expect, it } from "vitest";

import {
  displayNameProblem,
  isOwnAddress,
  learnName,
  learnNames,
  nameAt,
  nameOf,
  usernameProblem,
} from "./names";

describe("nameOf", () => {
  it("calls somebody by their display name", () => {
    expect(nameOf({ username: "amina", displayName: "Amina Juma" })).toBe("Amina Juma");
    expect(nameOf({ username: "amina", displayName: "  Amina Juma " })).toBe("Amina Juma");
  });

  it("falls back to the username when there is no display name", () => {
    expect(nameOf({ username: "amina" })).toBe("amina");
    expect(nameOf({ username: "amina", displayName: "" })).toBe("amina");
    expect(nameOf({ username: "amina", displayName: "   " })).toBe("amina");
    expect(nameOf({ username: "amina", displayName: null })).toBe("amina");
  });

  it("calls a guest by the name they gave", () => {
    expect(nameOf({ username: "guest-7f3a", displayName: "Visitor" })).toBe("Visitor");
  });
});

describe("nameAt", () => {
  it("calls somebody by the name it was told", () => {
    learnName("amina@tdc.test", "Amina Juma");
    expect(nameAt("amina@tdc.test")).toBe("Amina Juma");
  });

  it("learns from a list of people, falling back to the username", () => {
    learnNames([{ address: "baraka@tdc.test", username: "baraka", displayName: "  " }]);
    expect(nameAt("baraka@tdc.test")).toBe("baraka");
  });

  it("calls somebody it knows nothing of by their address's first half", () => {
    expect(nameAt("zawadi@elsewhere.test")).toBe("zawadi");
  });
});

describe("usernameProblem", () => {
  it("accepts what the server accepts", () => {
    for (const name of ["ab", "amina.j", "Ano_Rebel", "dev-42", "a".repeat(32)]) {
      expect(usernameProblem(name), name).toBeNull();
    }
  });

  it("names what is wrong", () => {
    expect(usernameProblem("a")).toBe("length");
    expect(usernameProblem("a".repeat(33))).toBe("length");
    expect(usernameProblem("amina juma")).toBe("characters");
    expect(usernameProblem("amína")).toBe("characters");
    expect(usernameProblem("amina@tdc")).toBe("characters");
    expect(usernameProblem(".amina")).toBe("edges");
    expect(usernameProblem("amina-")).toBe("edges");
  });
});

describe("displayNameProblem", () => {
  it("accepts names in any script, and empty", () => {
    for (const name of [
      "",
      "Amina Juma",
      "Ñandú",
      "李小龍",
      "Zawadi 🌱",
      "Neema 👩🏽\u200d💻",
      "Juma ❤️\u200d🔥",
    ]) {
      expect(displayNameProblem(name), name).toBeNull();
    }
  });

  it("refuses what can deceive or does not fit", () => {
    expect(displayNameProblem("Amina\u202eanimA")).toBe("characters");
    expect(displayNameProblem("Ami\u200bna")).toBe("characters");
    expect(displayNameProblem("Amina\u0007")).toBe("characters");
    expect(displayNameProblem("a".repeat(65))).toBe("length");
  });
});

describe("isOwnAddress", () => {
  const user = { address: "amina.j@tdc.test", formerAddresses: ["amina@tdc.test"] };

  it("recognises the current and the former addresses", () => {
    expect(isOwnAddress(user, "amina.j@tdc.test")).toBe(true);
    expect(isOwnAddress(user, "amina@tdc.test")).toBe(true);
  });

  it("recognises nobody else", () => {
    expect(isOwnAddress(user, "baraka@tdc.test")).toBe(false);
    expect(isOwnAddress(user, "")).toBe(false);
    expect(isOwnAddress(null, "amina@tdc.test")).toBe(false);
    expect(isOwnAddress({ address: "amina@tdc.test" }, "amina@tdc.test")).toBe(true);
  });
});
