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
