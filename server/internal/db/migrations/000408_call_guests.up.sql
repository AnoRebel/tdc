/*
Guests: people without an account let into one call through a link that
allows them, where the server allows guests at all.

A guest is an account row so that everything a call does — who is in it, who
sends to whom — works for them unchanged; guest_call says it is a guest's and
for which call. Such a row has no password and cannot sign in; its session
reaches that call and nothing else, and stops working when the call ends. It is
left out of every list and count of the server's people.
*/
ALTER TABLE users ADD COLUMN guest_call UUID REFERENCES calls(id) ON DELETE SET NULL;
CREATE INDEX users_guests ON users (guest_call) WHERE guest_call IS NOT NULL;
