import { expect, request as playwrightRequest, test } from "@playwright/test";
import type { APIRequestContext, Page } from "@playwright/test";

import {
  account,
  founderWithDirectory,
  onlyInDirectoryProjects,
  people,
  signInAs,
  placeIn,
} from "./directory.helpers";

/*
 * Two sets of settings, each deciding only what it says it decides — checked
 * from a phone, as the person looking.
 *
 * What a member list says about somebody follows their privacy settings: are
 * they online, their status, when they were last here. Who can find their
 * directory profile follows the profile's visibility. Neither may override the
 * other: hiding one's presence must not take a profile out of the directory,
 * and a profile anyone can find must not make somebody's presence visible.
 *
 * The server's own test walks every combination at the HTTP boundary. This one
 * takes the two opposite corners through the interface a client uses, and
 * reads the presence answers as the client's browser receives them — because a
 * detail that reaches the browser has been disclosed, whatever the screen does
 * with it.
 */

onlyInDirectoryProjects();
test.describe.configure({ mode: "serial" });

const developer = people.developer;
const client = people.client;
const place = `Kigoma${Date.now().toString(36)}`;
const presenceFields = ["online", "status", "statusText", "lastSeenAt"];

type Headers = { Authorization: string };

/** A channel both people are in, so each appears in the other's member list. */
async function sharedChannel(request: APIRequestContext, founder: Headers, people: Headers[]) {
  const slug = `compose-${Date.now().toString(36)}`;
  const made = await request.post("/api/rooms", {
    headers: founder,
    data: { slug, name: slug, visibility: "public" },
  });
  expect(made.status(), await made.text()).toBe(201);
  const roomId = (await made.json()).id as string;
  for (const headers of people) {
    const joined = await request.post(`/api/rooms/${roomId}/join`, { headers });
    expect(joined.ok(), await joined.text()).toBeTruthy();
  }
  return roomId;
}

/** The developer's entry in the member list and in room presence, as the client receives them. */
async function whatTheClientReceives(
  request: APIRequestContext,
  asClient: Headers,
  roomId: string,
) {
  const members = (await (
    await request.get(`/api/rooms/${roomId}/members`, { headers: asClient })
  ).json()) as Record<string, unknown>[];
  const member = members.find((entry) => entry.username === developer);
  expect(member, "the developer is in the member list").toBeTruthy();
  const presence = (await (
    await request.get(`/api/rooms/${roomId}/presence`, { headers: asClient })
  ).json()) as Record<string, unknown>[];
  const polled = presence.find((entry) => entry.userId === member!.id);
  expect(polled, "the developer is in the room's presence").toBeTruthy();
  return { member: member!, polled: polled! };
}

async function choose(
  request: APIRequestContext,
  asDeveloper: Headers,
  discloses: boolean,
  visibility: "public" | "members" | "hidden",
) {
  const privacy = await request.put("/api/me/privacy", {
    headers: asDeveloper,
    data: { showsPresence: discloses, showsStatus: discloses, showsLastSeen: discloses },
  });
  expect(privacy.status(), await privacy.text()).toBe(204);

  await placeIn(request, asDeveloper, place);
  const own = await (await request.get("/api/directory/me", { headers: asDeveloper })).json();
  const saved = await request.put("/api/directory/me", {
    headers: asDeveloper,
    data: {
      ...(own.profile ?? {}),
      headline: "Ledger developer",
      visibility,
      detailsVisibility: "public",
    },
  });
  expect(saved.ok(), await saved.text()).toBeTruthy();
}

/** Searches by this run's place, through the form, and opens the profile page. */
async function findInDirectory(page: Page) {
  await page.goto("/directory");
  await page.getByLabel("Location").fill(place.toLowerCase());
  await page.getByRole("button", { name: "Search", exact: true }).click();
  await expect(page.getByText(/Nobody matches|Ledger developer/).first()).toBeVisible({
    timeout: 15_000,
  });
  const found = (await page.getByRole("link", { name: new RegExp(developer) }).count()) > 0;

  await page.goto(`/directory/people/${developer}`);
  await expect(page.getByText(/No profile to show|Ledger developer/).first()).toBeVisible({
    timeout: 15_000,
  });
  const opens = (await page.getByText("Ledger developer").count()) > 0;
  return { found, opens };
}

test("hiding your presence leaves a findable profile findable", async ({ page, request }) => {
  const founder = await founderWithDirectory(request);
  const asDeveloper = await account(request, developer);
  const asClient = await account(request, client);
  const roomId = await sharedChannel(request, founder, [asDeveloper, asClient]);
  const before = await (await request.get("/api/directory/me", { headers: asDeveloper })).json();

  try {
    // Presence withheld, profile open to members.
    await request.put("/api/me/status", {
      headers: asDeveloper,
      data: { status: "busy", text: "on a deadline" },
    });
    await choose(request, asDeveloper, false, "members");

    // The member list and the presence the conversation polls say nothing.
    const { member, polled } = await whatTheClientReceives(request, asClient, roomId);
    for (const field of presenceFields) {
      expect(member, `the member list carries ${field}`).not.toHaveProperty(field);
    }
    for (const field of ["state", "status", "statusText", "lastSeenAt"]) {
      expect(polled, `room presence carries ${field}`).not.toHaveProperty(field);
    }

    // And the directory is untouched by it: the client finds the profile and
    // opens it, on their phone.
    await signInAs(page, client);
    const { found, opens } = await findInDirectory(page);
    expect(found, "a members-only profile is found by a member").toBe(true);
    expect(opens, "a members-only profile opens for a member").toBe(true);
    await expect(page.getByText("on a deadline")).toHaveCount(0);

    // Somebody not signed in is not shown a members-only profile, whatever
    // the presence settings say. A fresh context, since the shared one holds
    // whatever cookie the last sign-in through it set.
    const stranger = await playwrightRequest.newContext({
      baseURL: test.info().project.use.baseURL,
    });
    expect((await stranger.get(`/api/directory/profiles/${developer}`)).status()).toBe(404);
    await stranger.dispose();

    // Now the opposite corner: presence disclosed, profile hidden.
    await choose(request, asDeveloper, true, "hidden");

    const disclosed = await whatTheClientReceives(request, asClient, roomId);
    // Online is always answered when disclosed; the others only once there is
    // something to say, which is the member list's own rule.
    expect(disclosed.member, "the member list lacks online").toHaveProperty("online");
    for (const field of ["state", "status", "lastSeenAt"]) {
      expect(disclosed.polled, `room presence lacks ${field}`).toHaveProperty(field);
    }

    // Disclosing presence does not bring a hidden profile back.
    const hidden = await findInDirectory(page);
    expect(hidden.found, "a hidden profile is found in a search").toBe(false);
    expect(hidden.opens, "a hidden profile opens").toBe(false);

    // And a profile anybody can find never carries presence, whatever is
    // disclosed in the member list.
    await choose(request, asDeveloper, true, "public");
    const open = await request.get(`/api/directory/profiles/${developer}`, { headers: asClient });
    expect(open.status()).toBe(200);
    const profile = (await open.json()) as Record<string, unknown>;
    for (const field of presenceFields) {
      expect(profile, `the directory profile carries ${field}`).not.toHaveProperty(field);
    }
  } finally {
    // Left as the other journeys expect to find this person.
    await request.put("/api/me/privacy", {
      headers: asDeveloper,
      data: { showsPresence: true, showsStatus: true, showsLastSeen: true },
    });
    await request.put("/api/me/status", {
      headers: asDeveloper,
      data: { status: "available", text: "" },
    });
    if (before.profile) {
      await request.put("/api/directory/me", { headers: asDeveloper, data: before.profile });
    }
  }
});
