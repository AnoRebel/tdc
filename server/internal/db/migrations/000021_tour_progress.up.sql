/*
Which introductions somebody has already been given.

On the account rather than in a browser, so an introduction shown on the web
client is not shown again on the desktop one. A tour is worth giving to
somebody who has not seen it and an obstacle to everybody else, and "everybody
else" includes the same person on their second device.

One JSONB column rather than a column or a row per tour. The set of tours grows
as the application does — every phase that adds a screen may add an
introduction to it — and a shape that needed a migration for each would make
adding a tour a schema change. What is stored is small, opaque to the database,
and read whole.

Empty object rather than null for the same reason the appearance columns are
empty strings: absent and deliberately-empty are the same thing here, and one
representation is one fewer state to get wrong.
*/
ALTER TABLE users
  ADD COLUMN tour_progress JSONB NOT NULL DEFAULT '{}'::jsonb;

/*
An object, not an array or a scalar.

The client reads this into a structure with named fields, and a value of some
other shape would be a runtime error in the interface rather than a refusal
here. This is written through an API a program may call directly, so the
constraint is worth having.
*/
ALTER TABLE users
  ADD CONSTRAINT users_tour_progress_is_object
    CHECK (jsonb_typeof(tour_progress) = 'object');
