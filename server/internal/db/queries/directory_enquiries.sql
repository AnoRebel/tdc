/*
Enquiries.
*/

-- name: EnquiryRecipient :one
-- Whom an enquiry to this username would reach, and whether they take one
-- from this sender: their profile must be one the sender may see, it must take
-- enquiries, and they must not have blocked the sender. One row either way, so
-- every refusal takes the same path.
SELECT
    u.id,
    u.username,
    u.display_name,
    (p.user_id IS NOT NULL
     AND p.profile_rank <= @viewer_rank::smallint
     AND COALESCE(p.accepts_enquiries, p.visibility = 'public')
     AND NOT EXISTS (SELECT 1 FROM user_blocks b
                     WHERE b.blocker_id = u.id AND b.blocked_id = @sender::uuid))::boolean AS accepts
FROM users u
LEFT JOIN directory_profiles p ON p.user_id = u.id
WHERE u.id = user_named(lower(@username::text));

-- name: CreateEnquiryRoom :one
-- The private conversation an enquiry opens. A direct room, so everything that
-- keeps direct conversations out of search, discovery and counts keeps this
-- out too; the participant key names the enquiry, so it never collides with
-- the ordinary direct conversation between the same two people.
INSERT INTO rooms (id, name, kind, visibility, home_server, created_by, participant_key)
VALUES (@id, @name, 'direct', 'private', @home_server, @created_by, @participant_key)
RETURNING *;

-- name: DeleteEnquiryRoom :exec
-- Only for undoing an enquiry that failed half way; never anybody's room.
DELETE FROM rooms WHERE id = $1 AND participant_key LIKE 'enquiry:%';

-- name: InsertEnquiry :one
INSERT INTO enquiries (id, sender_id, recipient_id, subject, body, room_id)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: ListReceivedEnquiries :many
SELECT e.*, u.username AS other_username, u.display_name AS other_display_name,
       -- The nil identifier where the enquiry was not taken up.
       COALESCE((SELECT g.id FROM engagements g WHERE g.enquiry_id = e.id),
                '00000000-0000-0000-0000-000000000000')::uuid AS engagement_id
FROM enquiries e
JOIN users u ON u.id = e.sender_id
WHERE e.recipient_id = $1
ORDER BY e.created_at DESC
LIMIT 100;

-- name: ListSentEnquiries :many
SELECT e.*, u.username AS other_username, u.display_name AS other_display_name,
       -- The nil identifier where the enquiry was not taken up.
       COALESCE((SELECT g.id FROM engagements g WHERE g.enquiry_id = e.id),
                '00000000-0000-0000-0000-000000000000')::uuid AS engagement_id
FROM enquiries e
JOIN users u ON u.id = e.recipient_id
WHERE e.sender_id = $1
ORDER BY e.created_at DESC
LIMIT 100;

-- name: GetEnquiry :one
SELECT * FROM enquiries WHERE id = $1;

-- name: SetEnquiryStatus :execrows
-- Answering an enquiry, once: only a pending one moves.
UPDATE enquiries SET status = @status, responded_at = NOW()
WHERE id = @id AND status = 'pending';
