/*
What the interface looks like, per person.

Three columns rather than one, because they are three independent choices: a
palette, an appearance, and an accent. Somebody who likes the warm palette in
dark with their own accent has made three decisions, and one column holding
their combination would have to enumerate the product.

Stored here rather than only in a browser so the choice follows somebody
between clients — the point of having a desktop application that shows the same
interface is that it shows the same interface.

Empty means "whatever the operator chose", which is why these are text with an
empty default rather than nullable: absent and deliberately-empty are the same
thing here, and one representation is fewer states to get wrong.
*/
ALTER TABLE users
  ADD COLUMN appearance_palette TEXT NOT NULL DEFAULT '',
  ADD COLUMN appearance_mode TEXT NOT NULL DEFAULT '',
  ADD COLUMN appearance_accent TEXT NOT NULL DEFAULT '';

/*
The values the application understands, refused here as well as in the handler.

A check constraint rather than trust: these are written by an API a program may
call directly, and a palette name nothing matches would leave somebody with an
attribute selecting no rules — an interface that is subtly the default and
gives no reason why.

The accent is not constrained here. Its validity is a contrast ratio against a
foreground, which is not something a column can express, so it is checked where
it is set.
*/
ALTER TABLE users
  ADD CONSTRAINT users_appearance_palette_known
    CHECK (appearance_palette IN ('', 'default', 'congrator')),
  ADD CONSTRAINT users_appearance_mode_known
    CHECK (appearance_mode IN ('', 'light', 'dark', 'auto'));
