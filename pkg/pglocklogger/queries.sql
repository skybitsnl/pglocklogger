-- name: GetActivity :many
WITH RECURSIVE activities AS (
	-- Start with all activity
    SELECT
        activity.*,
        pg_blocking_pids(activity.pid)::int4[] as blocked_by
    FROM pg_catalog.pg_stat_activity activity
    WHERE wait_event_type='Lock'
    UNION
	-- In a recursive query, request all activity blocked on a lock,
	-- plus the PIDs they are blocked on
    SELECT
        activity.*,
        pg_blocking_pids(activity.pid)::int4[] as blocked_by
    FROM pg_catalog.pg_stat_activity activity, activities
	WHERE activity.pid=any(activities.blocked_by)
)
SELECT
    pid, state, blocked_by, wait_event_type, wait_event,
    query, backend_type, datname, usename, application_name,
    client_addr, client_hostname, client_port, current_timestamp::timestamptz AS current_timestamp,
    backend_start, xact_start, query_start, state_change,
    backend_xid
FROM activities;

-- name: GetLocksForPids :many
SELECT l.pid, l.locktype, l.mode, l.granted, l.waitstart, l.relation,
    l.page, l.tuple, l.virtualxid, l.transactionid, l.classid,
    l.objid, l.objsubid, l.virtualtransaction,
    rel.relname, coalesce(rel.relkind, '')::text AS relkind
FROM pg_catalog.pg_locks l
LEFT JOIN pg_catalog.pg_class rel ON rel.oid=l.relation
WHERE l.pid=ANY(sqlc.arg(pids)::oid[]);
