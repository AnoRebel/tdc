/*
Enquiries: the first contact a client makes through somebody's profile.

An enquiry is not a direct message, and the two are governed separately on
purpose. Who may open a conversation with somebody is user_privacy's
direct_messages, a setting about people in general; whether somebody wants to
be approached about work through the directory is a question only their
directory profile can answer. Closing direct messages does not close enquiries,
and opening a profile to enquiries does not reopen direct messages.
*/

/*
Whether a profile takes enquiries.

NULL means the owner has not chosen, and then it follows the profile: a public
profile takes them, because a profile advertised to strangers that nobody could
contact would be a dead end, and anything less than public does not. Once the
owner chooses, the choice stands whatever the visibility does afterwards.
*/
ALTER TABLE directory_profiles ADD COLUMN accepts_enquiries BOOLEAN;

/*
One enquiry: who asked whom, about what.

The text is kept here as well as in the conversation it opens, because this is
the record both people's enquiry lists read, and it must still say what was
asked after the conversation's own retention has done its work.

The conversation — a private room only the two of them are ever in — is where
the answer happens. Until the recipient accepts, only they are in it, holding
the one message the enquiry is: the sender has said their piece, and saying
more is the recipient's decision, not theirs.
*/
CREATE TABLE enquiries (
    id           UUID        PRIMARY KEY,
    sender_id    UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    recipient_id UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    subject      TEXT        NOT NULL,
    body         TEXT        NOT NULL,
    room_id      UUID        REFERENCES rooms(id) ON DELETE SET NULL,
    /*
      'pending' until the recipient answers; 'accepted' when they take it up,
      which is what makes it an engagement; 'declined' when they do not; and
      'withdrawn' when the sender takes it back first.
    */
    status       TEXT        NOT NULL DEFAULT 'pending',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    responded_at TIMESTAMPTZ,

    CONSTRAINT enquiries_not_to_oneself CHECK (sender_id <> recipient_id),
    CONSTRAINT enquiries_status_valid
        CHECK (status IN ('pending', 'accepted', 'declined', 'withdrawn')),
    CONSTRAINT enquiries_subject_not_empty CHECK (length(trim(subject)) > 0),
    CONSTRAINT enquiries_body_not_empty CHECK (length(trim(body)) > 0)
);

-- One unanswered enquiry from one person to another at a time: asking again
-- before an answer is pressure, not a second question.
CREATE UNIQUE INDEX enquiries_one_pending_idx
    ON enquiries (sender_id, recipient_id) WHERE status = 'pending';

CREATE INDEX enquiries_recipient_idx ON enquiries (recipient_id, created_at DESC);
CREATE INDEX enquiries_sender_idx ON enquiries (sender_id, created_at DESC);
