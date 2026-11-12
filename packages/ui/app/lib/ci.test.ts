import { readFileSync } from "node:fs";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

/*
 * Every CI job is a required check.
 *
 * Branch protection points at one job — `ci-success` — rather than a list that
 * has to be updated by hand, which is the right shape. The failure mode is that
 * somebody adds a job, forgets to add it to that list, and it runs on every pull
 * request while blocking nothing: green tick, failing job, merged anyway.
 *
 * Nothing about YAML catches that, and nobody notices until the thing the job
 * was protecting against reaches main.
 */

const WORKFLOW = join(
  import.meta.dirname,
  "..",
  "..",
  "..",
  "..",
  ".github",
  "workflows",
  "ci.yml",
);

/** The jobs a workflow defines, read without a YAML parser. */
function jobsIn(workflow: string): string[] {
  const body = workflow.split(/^jobs:\s*$/m)[1] ?? "";
  // A job is a key indented exactly two spaces under `jobs:`.
  return [...body.matchAll(/^ {2}([a-z][\w-]*):$/gm)].map((match) => match[1]!);
}

/** What `ci-success` waits for. */
function requiredBy(workflow: string): string[] {
  const needs = workflow.match(
    /^ {2}ci-success:[\s\S]*?^ {4}needs:\s*(\[[^\]]*\]|[\s\S]*?)^ {4}\w/m,
  );
  if (!needs) return [];

  return [...(needs[1] ?? "").matchAll(/[a-z][\w-]*/g)].map((match) => match[0]);
}

describe("continuous integration", () => {
  it("makes every job a required check", () => {
    const workflow = readFileSync(WORKFLOW, "utf8");

    const required = requiredBy(workflow);
    expect(
      required.length,
      "ci-success declares no needs, so nothing blocks a merge",
    ).toBeGreaterThan(0);

    const missing = jobsIn(workflow).filter(
      // `tier` decides which jobs run, and `ci-success` is the check itself.
      (job) => job !== "tier" && job !== "ci-success" && !required.includes(job),
    );

    expect(
      missing,
      "these jobs run but block nothing — a failure in one merges with a green tick",
    ).toEqual([]);
  });
});
