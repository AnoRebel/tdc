/*
A guest's call is named without a foreign key.

users → calls closed a loop with calls → users (who started a call, who
presents in it), and a loop of references is one no export can order: every
table in it waits for another to be written first. Calls are ended, never
deleted, so the reference has nothing to protect against that a constraint
would catch; the column and its index stay.
*/
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_guest_call_fkey;
