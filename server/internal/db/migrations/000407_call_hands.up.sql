-- When somebody raised their hand in a call, so hands are taken in order;
-- null while it is down.
ALTER TABLE call_participants ADD COLUMN hand_raised_at TIMESTAMPTZ;
