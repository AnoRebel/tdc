# What was taken from elsewhere, and what was not

## Inspira UI

Reviewed rather than adopted. It is MIT-licensed, actively maintained, and has
155 components — and almost none of them belong in a chat application.

The catalogue is built for landing pages: black holes, particle whirlpools,
cosmic portals, ten animated backgrounds, twelve shader and 3D effects. That is
not a criticism of the project. It is a statement that it solves a different
problem from this one.

### What the review found

Reading the source before adopting anything is the whole point of the exercise,
and it paid for itself on the first component.

`AnimatedList` — the closest thing to something a chat application might want —
starts a `while (true)` loop in `onMounted` with no cleanup. It keeps cycling
after the component is unmounted, for as long as the tab is open. In an
application people leave open all day, on a phone, that is a battery drain
nobody would trace back to a decorative list.

`NumberTicker` is well written and does the wrong thing here. It counts a value
up over a second, which is a flourish on a landing page and a defect on an
unread badge: the number is wrong for as long as the animation runs, and
somebody glancing at it reads a stale figure.

### What it would have cost

Anything genuinely animated in the catalogue needs `motion-v`, which brings
framer-motion and three more packages — 548 KB against 19 KB of headroom in
the initial-load budget. For decoration, in an application whose users may be
on a metered connection.

### What was taken instead

Two ideas, written against CSS transitions and Vue's own `TransitionGroup`:

- **`Count`** — a number that changes immediately and a badge that acknowledges
  it moved. Somebody needs the figure at a glance; what the animation adds is
  noticing that it changed, not the changing itself.
- **`Arrive`** — a message that fades in from slightly below, so it reads as
  having come from somewhere rather than the interface redrawing itself.

Both clean up after themselves, both honour `prefers-reduced-motion`, and both
cost nothing: the initial-load budget did not move.

`count.test.ts` holds the rules the originals broke, so a future change cannot
quietly reintroduce them.

## shadcn-vue

Adopted, by copying. The components in `ui/` are vendored rather than depended
on, which is how shadcn-vue is meant to be used — the alternative is a
dependency whose next release decides what this application looks like.
