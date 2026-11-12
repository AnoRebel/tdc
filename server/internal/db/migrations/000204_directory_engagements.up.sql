/*
Engagements: an enquiry somebody took up, from discussion to completed work.

The server keeps the record of what the two people did and nothing more. It is
not a party to the work, and no state here means the server judged anything:
"completed" means both of them said so.

Parties are kept by identifier and by name. The identifier goes if an account
is deleted — the rest of the record belongs to the other person too, and is
theirs to keep — and the name stays, so the record still says who it was with.
*/
CREATE TABLE engagements (
    id              UUID        PRIMARY KEY,
    enquiry_id      UUID        UNIQUE REFERENCES enquiries(id) ON DELETE SET NULL,
    -- Who asked, and who was asked.
    client_id       UUID        REFERENCES users(id) ON DELETE SET NULL,
    developer_id    UUID        REFERENCES users(id) ON DELETE SET NULL,
    client_label    TEXT        NOT NULL,
    developer_label TEXT        NOT NULL,
    title           TEXT        NOT NULL,
    -- The private conversation, which the enquiry opened.
    room_id         UUID        REFERENCES rooms(id) ON DELETE SET NULL,
    state           TEXT        NOT NULL,
    -- Who asked for the work to be recorded as complete, while it waits for
    -- the other to agree.
    completion_requested_by UUID REFERENCES users(id) ON DELETE SET NULL,
    completion_requested_at TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT engagements_state_valid CHECK (state IN (
        'discussion', 'proposal', 'negotiation', 'agreement',
        'active', 'completion_pending', 'completed', 'withdrawn'
    ))
);

CREATE INDEX engagements_client_idx ON engagements (client_id, updated_at DESC);
CREATE INDEX engagements_developer_idx ON engagements (developer_id, updated_at DESC);

/*
Every change of state, with who caused it and when, visible to both people.

The pairs a state may move between are written into the table itself, so that
no code path — a later one, a hand-written update — can record a step that
skips another. The first row of every engagement is the enquiry being taken up.
*/
CREATE TABLE engagement_transitions (
    id            BIGSERIAL   PRIMARY KEY,
    engagement_id UUID        NOT NULL REFERENCES engagements(id) ON DELETE CASCADE,
    from_state    TEXT        NOT NULL,
    to_state      TEXT        NOT NULL,
    actor_id      UUID        REFERENCES users(id) ON DELETE SET NULL,
    actor_label   TEXT        NOT NULL,
    note          TEXT        NOT NULL DEFAULT '',
    occurred_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT engagement_transitions_allowed CHECK ((from_state, to_state) IN (
        ('enquiry', 'discussion'),
        ('discussion', 'proposal'),
        ('proposal', 'negotiation'),
        ('negotiation', 'proposal'),
        ('proposal', 'agreement'),
        ('negotiation', 'agreement'),
        ('agreement', 'active'),
        ('active', 'completion_pending'),
        ('completion_pending', 'completed'),
        ('discussion', 'withdrawn'),
        ('proposal', 'withdrawn'),
        ('negotiation', 'withdrawn')
    ))
);

CREATE INDEX engagement_transitions_engagement_idx ON engagement_transitions (engagement_id, id);
