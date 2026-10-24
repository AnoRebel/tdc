-- name: IndexMessage :exec
INSERT INTO message_search (event_id, room_id, sender_id, sender_address, body, sent_at)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (event_id) DO UPDATE
SET body = EXCLUDED.body;

-- name: UnindexMessage :exec
DELETE FROM message_search WHERE event_id = $1;

-- Full-text search, restricted to rooms the caller is in.
--
-- The membership join is the authorisation, done in the query rather than by
-- filtering afterwards: a result that is fetched and then discarded has still
-- been read from disk, and a mistake in the filtering would leak it.
--
-- websearch_to_tsquery never raises a syntax error, whatever is typed at it,
-- which is why hostile input needs no sanitising here.
-- name: SearchMessages :many
SELECT s.event_id, s.room_id, s.sender_address, s.body, s.sent_at,
       ts_headline('english', s.body, websearch_to_tsquery('english', @query::text),
                   'StartSel=<mark>, StopSel=</mark>, MaxFragments=2, MaxWords=18, MinWords=5')
           AS highlighted
FROM message_search s
JOIN room_members m ON m.room_id = s.room_id AND m.user_id = @user_id
WHERE s.search_vector @@ websearch_to_tsquery('english', @query::text)
  AND (@all_rooms::boolean OR s.room_id = @room_id::uuid)
ORDER BY s.sent_at DESC
LIMIT @result_limit;

-- Prefix and fuzzy matching, for search-as-you-type.
--
-- websearch_to_tsquery cannot match a prefix, so "mkut" finds nothing there.
-- Trigram similarity finds it, and tolerates a typo at the same time.
-- name: SearchMessagesFuzzy :many
SELECT s.event_id, s.room_id, s.sender_address, s.body, s.sent_at,
       similarity(s.body, @query::text) AS score
FROM message_search s
JOIN room_members m ON m.room_id = s.room_id AND m.user_id = @user_id
WHERE (
        s.body ILIKE '%' || @query::text || '%'
        OR similarity(s.body, @query::text) > 0.2
      )
  AND (@all_rooms::boolean OR s.room_id = @room_id::uuid)
ORDER BY s.sent_at DESC
LIMIT @result_limit;

-- Rooms a person may find, by name or topic.
--
-- No index is added for this, deliberately. A server has hundreds of channels
-- where it has millions of messages, so Postgres reads the table and is done
-- before an index lookup would have finished planning — and an index here
-- would cost storage and a write on every rename for nothing.
--
-- Visibility is the authorisation: a private channel appears only to somebody
-- already in it, so searching cannot be used to discover that one exists.
-- name: SearchRooms :many
SELECT r.id, r.slug, r.name, r.topic, r.kind, r.visibility,
       (r.archived_at IS NOT NULL) AS archived,
       (m.user_id IS NOT NULL) AS is_member
FROM rooms r
LEFT JOIN room_members m ON m.room_id = r.id AND m.user_id = @user_id
WHERE r.kind NOT IN ('direct', 'project', 'issue-discussion', 'event')
  AND (r.visibility = 'public' OR m.user_id IS NOT NULL)
  AND (
        r.name ILIKE '%' || @query::text || '%'
        OR r.slug ILIKE '%' || @query::text || '%'
        OR coalesce(r.topic, '') ILIKE '%' || @query::text || '%'
      )
ORDER BY (m.user_id IS NOT NULL) DESC, r.name
LIMIT @result_limit;

-- People, by username or display name.
--
-- Only what a member list already discloses: a name and an address. Anything
-- somebody has chosen to withhold stays withheld, and search must not become
-- the way around a privacy setting.
-- name: SearchPeople :many
SELECT id, username, display_name
FROM users
WHERE (username ILIKE '%' || @query::text || '%'
   OR display_name ILIKE '%' || @query::text || '%')
  -- Guests of a call are nobody to find.
  AND guest_call IS NULL
ORDER BY
  -- An exact match first, then a prefix, then anything containing it. Somebody
  -- typing a whole username wants that person, not the twelve others whose
  -- names contain it.
  (lower(username) = lower(@query::text)) DESC,
  (username ILIKE @query::text || '%') DESC,
  username
LIMIT @result_limit;
