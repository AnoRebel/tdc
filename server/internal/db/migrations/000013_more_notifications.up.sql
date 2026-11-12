-- Notifications for reactions, joins, replies, forwards and pins.
--
-- The existing table covered mentions, direct messages and threads: everything
-- that arrives as a message. The things people also want to hear about are not
-- messages — somebody reacting, somebody joining, somebody pinning what you
-- wrote — and each needed something the table did not have.

-- Who caused it.
--
-- A message notification could take the actor from the event it points at. A
-- reaction cannot: the event is the message that was reacted to, whose sender
-- is usually the person being notified, so without this the notification would
-- say you reacted to yourself.
ALTER TABLE notifications ADD COLUMN actor_address TEXT NOT NULL DEFAULT '';

-- What was reacted with, for the one reason that needs it.
--
-- Kept here rather than looked up, because a reaction that has been taken back
-- should still explain the notification it raised — otherwise a person opening
-- their notifications sees "reacted to your message" with nothing to show.
ALTER TABLE notifications ADD COLUMN detail TEXT NOT NULL DEFAULT '';

ALTER TABLE notifications DROP CONSTRAINT IF EXISTS notifications_reason_check;
ALTER TABLE notifications ADD CONSTRAINT notifications_reason_check
    CHECK (reason IN (
        'mention', 'direct', 'thread',
        -- Somebody reacted to something you wrote.
        'reaction',
        -- Somebody replied in a thread you started.
        'reply',
        -- Somebody joined a conversation you are in.
        'join',
        -- Something you wrote was forwarded elsewhere.
        'forward',
        -- Something you wrote was pinned.
        'pin'
    ));

/*
 * One notification per person per event per reason and actor.
 *
 * The old rule was one per person per event, which was right while every
 * notification was about a message arriving: an edit or a redelivery must not
 * notify twice.
 *
 * It is wrong for the rest. Two people reacting to the same message are two
 * things worth hearing about, and under the old constraint the second was
 * silently dropped. Including the actor and the reason makes each distinct,
 * while still collapsing the same person doing the same thing twice.
 */
ALTER TABLE notifications DROP CONSTRAINT IF EXISTS notifications_once;
ALTER TABLE notifications ADD CONSTRAINT notifications_once
    UNIQUE (user_id, event_id, reason, actor_address);
