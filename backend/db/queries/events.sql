-- name: ReadFlagEventCursor :one
SELECT submit_time
FROM StolenFlags
WHERE flag_id = sqlc.arg(flag_id)::UUID
  AND attacker_id = sqlc.arg(attacker_id)::UUID;

-- name: ReadAnnouncementEventCursor :one
SELECT created_at
FROM PublicAnnouncements
WHERE id = sqlc.arg(announcement_id)::UUID;

-- name: ListLatestPublicEvents :many
WITH recent_flags AS (
    SELECT flag_id, attacker_id, submit_time, attacker_delta
    FROM StolenFlags
    WHERE submit_time IS NOT NULL
    ORDER BY submit_time DESC, flag_id DESC, attacker_id DESC
    LIMIT sqlc.arg(page_size)::INTEGER
), recent_announcements AS (
    SELECT id, title, markdown, author, created_at
    FROM PublicAnnouncements
    ORDER BY created_at DESC, id DESC
    LIMIT sqlc.arg(page_size)::INTEGER
), events AS (
    SELECT ('flag_stolen:' || stolen.flag_id::TEXT || ':' || stolen.attacker_id::TEXT)::TEXT AS id,
           stolen.submit_time AS created_at,
           jsonb_strip_nulls(jsonb_build_object(
               'id', 'flag_stolen:' || stolen.flag_id::TEXT || ':' || stolen.attacker_id::TEXT,
               'kind', 'flag_stolen',
               'created_at', stolen.submit_time,
               'attacker_id', stolen.attacker_id,
               'attacker_name', attacker.name,
               'attacker_delta', stolen.attacker_delta,
               'victim_id', flag.team_id,
               'victim_name', victim.name,
               'task_id', flag.task_id,
               'task_name', COALESCE(task.name, ''),
               'round', flag.round
           )) AS data
    FROM recent_flags AS stolen
    JOIN Flags AS flag ON flag.id = stolen.flag_id
    JOIN Teams AS attacker ON attacker.id = stolen.attacker_id
    JOIN Teams AS victim ON victim.id = flag.team_id
    JOIN Tasks AS task ON task.id = flag.task_id
    UNION ALL
    SELECT ('admin_message:' || announcement.id::TEXT)::TEXT AS id,
           announcement.created_at,
           jsonb_build_object(
               'id', 'admin_message:' || announcement.id::TEXT,
               'kind', 'admin_message',
               'created_at', announcement.created_at,
               'title', announcement.title,
               'markdown', announcement.markdown,
               'author', announcement.author
           ) AS data
    FROM recent_announcements AS announcement
)
SELECT data::TEXT AS payload
FROM events
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(page_size)::INTEGER;

-- name: ListPublicEventsAfter :many
WITH recent_flags AS (
    SELECT flag_id, attacker_id, submit_time, attacker_delta
    FROM StolenFlags
    WHERE submit_time IS NOT NULL
      AND submit_time >= sqlc.arg(cursor_time)::TIMESTAMPTZ
      AND (submit_time, ('flag_stolen:' || flag_id::TEXT || ':' || attacker_id::TEXT)) >
          (sqlc.arg(cursor_time)::TIMESTAMPTZ, sqlc.arg(event_id)::TEXT)
    ORDER BY submit_time, flag_id, attacker_id
    LIMIT sqlc.arg(page_size)::INTEGER
), recent_announcements AS (
    SELECT id, title, markdown, author, created_at
    FROM PublicAnnouncements
    WHERE created_at >= sqlc.arg(cursor_time)::TIMESTAMPTZ
      AND (created_at, ('admin_message:' || id::TEXT)) >
          (sqlc.arg(cursor_time)::TIMESTAMPTZ, sqlc.arg(event_id)::TEXT)
    ORDER BY created_at, id
    LIMIT sqlc.arg(page_size)::INTEGER
), events AS (
    SELECT ('flag_stolen:' || stolen.flag_id::TEXT || ':' || stolen.attacker_id::TEXT)::TEXT AS id,
           stolen.submit_time AS created_at,
           jsonb_strip_nulls(jsonb_build_object(
               'id', 'flag_stolen:' || stolen.flag_id::TEXT || ':' || stolen.attacker_id::TEXT,
               'kind', 'flag_stolen',
               'created_at', stolen.submit_time,
               'attacker_id', stolen.attacker_id,
               'attacker_name', attacker.name,
               'attacker_delta', stolen.attacker_delta,
               'victim_id', flag.team_id,
               'victim_name', victim.name,
               'task_id', flag.task_id,
               'task_name', COALESCE(task.name, ''),
               'round', flag.round
           )) AS data
    FROM recent_flags AS stolen
    JOIN Flags AS flag ON flag.id = stolen.flag_id
    JOIN Teams AS attacker ON attacker.id = stolen.attacker_id
    JOIN Teams AS victim ON victim.id = flag.team_id
    JOIN Tasks AS task ON task.id = flag.task_id
    UNION ALL
    SELECT ('admin_message:' || announcement.id::TEXT)::TEXT AS id,
           announcement.created_at,
           jsonb_build_object(
               'id', 'admin_message:' || announcement.id::TEXT,
               'kind', 'admin_message',
               'created_at', announcement.created_at,
               'title', announcement.title,
               'markdown', announcement.markdown,
               'author', announcement.author
           ) AS data
    FROM recent_announcements AS announcement
)
SELECT event.data::TEXT AS payload
FROM events AS event
ORDER BY event.created_at, event.id
LIMIT sqlc.arg(page_size)::INTEGER;

-- name: InsertPublicAnnouncement :one
INSERT INTO PublicAnnouncements (title, markdown, author)
VALUES (sqlc.arg(title), sqlc.arg(markdown), sqlc.arg(author))
RETURNING id, title, markdown, author, created_at;

-- name: UpdateStolenFlagDeltas :execrows
WITH changes AS (
    SELECT flags.flag_id, deltas.attacker_delta
    FROM UNNEST(sqlc.arg(flag_ids)::UUID[]) WITH ORDINALITY AS flags(flag_id, ordinal)
    JOIN UNNEST(sqlc.arg(deltas)::DOUBLE PRECISION[]) WITH ORDINALITY AS deltas(attacker_delta, ordinal)
        USING (ordinal)
)
UPDATE StolenFlags AS stolen
SET attacker_delta = changes.attacker_delta
FROM changes
WHERE stolen.flag_id = changes.flag_id
  AND stolen.attacker_id = sqlc.arg(attacker_id)::UUID;
