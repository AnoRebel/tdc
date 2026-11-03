## Why

An agreement in the directory is recorded once and can never change, which is right for evidence and wrong for work: scope grows, dates move, prices are renegotiated. Today the only way to change the terms is to talk about it in the conversation, with nothing recorded that both people accepted.

## What Changes

- **Revising an agreement in force.** Either party proposes a revision — a reason and amended terms (scope, timeline, currency, deliverables and prices) — once there is an agreement and until the work is confirmed complete.
- **The same negotiation rules.** Each version of a revision is immutable; either party accepts, declines, asks for a change to one part, or sends a counter-version. A revision comes into force only when both parties accept the same version: a new agreement is recorded beside the earlier ones and becomes the one in force. Until then the agreement already recorded stands.
- **Ending a revision.** Declined by either party, or withdrawn by whoever opened it; either way the agreement in force is unchanged.
- **One at a time, settled before completion.** A second revision cannot be opened while one is open, and the work cannot be marked or confirmed complete while one is.
- **History.** Every agreement and every revision — who proposed it and why, each version, each answer, how it ended — visible to both parties with what each version changed, notified to the other party at each step, and included in the agreement export.
- **Guards in the database.** Revisions and their outcomes are insert-only; an answer from somebody who is not a party is refused; an agreement recorded without both parties' acceptance of that exact version is refused.

**Out of scope:** writing revisions to the server audit log (it would tell administrators an engagement exists, which only its parties may know; the engagement's own immutable record is the trail), and outgoing webhook events (the directory emits none today).

## Capabilities

### Modified Capabilities

- `directory/engagements`: a new requirement, Revising an agreement.

## Impact

- **Database:** migration 000428 adds `agreement_revisions` and `agreement_revision_outcomes`, a revision reference on proposal versions and agreements, allows several agreements per engagement (one per revision), and adds two insert guards.
- **API:** `POST /api/directory/engagements/:id/revisions`, `POST /api/directory/engagements/:id/revisions/:number/withdraw`; counter-versions and answers use the existing proposal routes; the negotiation read and the export gain `agreements` and `revisions`, additively.
