/*
What each person lets a member list say about them.

A channel's members can see who else is in it — the spec makes a name and an
address unconditional, because they are how a message is attributed and a
conversation is opened. Everything past that is the person's own to withhold.

On user_privacy rather than users, for the reason that table was created with:
these are per-person preferences, and a column on users for each would mean a
migration every time somebody wants to withhold one more thing. They also sit
beside direct_messages, which is the same kind of decision — who gets to learn
what about me.

Standing preferences, deliberately distinct from user_presence.invisible.
Invisible is a choice about this moment — appear offline while still connected,
and it lapses when the person turns it off. These hold until changed. Somebody
who has withheld their presence is not "currently invisible"; they have said
the question is not answered for them.

Defaults disclose, and the table is sparse: a person who has never opened these
settings has no row at all, so every read must supply the default rather than
assume a row exists. A server whose members cannot see who is around is not the
product this is, and a default of silence would have made the member list
useless on the day it shipped. The setting exists so somebody can choose
otherwise, and the choosing is what it records.
*/
ALTER TABLE user_privacy
  /*
    Whether others may see this person as online or offline at all.

    Withholding it does not disconnect them or change what they receive. It
    stops the server answering the question.
  */
  ADD COLUMN shows_presence  BOOLEAN NOT NULL DEFAULT TRUE,
  /*
    The status they have chosen, and its accompanying text.

    One flag for both: the text explains the status, and a status without its
    text ("away" with no "back at four") discloses the fact while withholding
    the part that made it useful. Splitting them would offer a choice nobody
    asked for.
  */
  ADD COLUMN shows_status    BOOLEAN NOT NULL DEFAULT TRUE,
  /*
    When they were last seen.

    The most revealing of the three, and the one people most often want off: a
    last-seen time is a record of when somebody sleeps, works, and stops
    working. Separate from presence because "online now" and "last here at
    03:12" disclose quite different things.
  */
  ADD COLUMN shows_last_seen BOOLEAN NOT NULL DEFAULT TRUE;
