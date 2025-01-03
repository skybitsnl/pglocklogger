// Code generated by sqlc. DO NOT EDIT.
// versions:
//   sqlc v1.27.0
// source: queries.sql

package gendb

import (
	"context"
	"net/netip"

	"github.com/jackc/pgx/v5/pgtype"
)

const getActivity = `-- name: GetActivity :many
WITH RECURSIVE activities AS (
	-- Start with all activity
    SELECT
        activity.datid, activity.datname, activity.pid, activity.leader_pid, activity.usesysid, activity.usename, activity.application_name, activity.client_addr, activity.client_hostname, activity.client_port, activity.backend_start, activity.xact_start, activity.query_start, activity.state_change, activity.wait_event_type, activity.wait_event, activity.state, activity.backend_xid, activity.backend_xmin, activity.query_id, activity.query, activity.backend_type,
        pg_blocking_pids(activity.pid)::int4[] as blocked_by
    FROM pg_catalog.pg_stat_activity activity
    WHERE wait_event_type='Lock'
    UNION
	-- In a recursive query, request all activity blocked on a lock,
	-- plus the PIDs they are blocked on
    SELECT
        activity.datid, activity.datname, activity.pid, activity.leader_pid, activity.usesysid, activity.usename, activity.application_name, activity.client_addr, activity.client_hostname, activity.client_port, activity.backend_start, activity.xact_start, activity.query_start, activity.state_change, activity.wait_event_type, activity.wait_event, activity.state, activity.backend_xid, activity.backend_xmin, activity.query_id, activity.query, activity.backend_type,
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
FROM activities
`

type GetActivityRow struct {
	Pid              pgtype.Int4
	State            pgtype.Text
	BlockedBy        []int32
	WaitEventType    pgtype.Text
	WaitEvent        pgtype.Text
	Query            pgtype.Text
	BackendType      pgtype.Text
	Datname          pgtype.Text
	Usename          pgtype.Text
	ApplicationName  pgtype.Text
	ClientAddr       *netip.Addr
	ClientHostname   pgtype.Text
	ClientPort       pgtype.Int4
	CurrentTimestamp pgtype.Timestamptz
	BackendStart     pgtype.Timestamptz
	XactStart        pgtype.Timestamptz
	QueryStart       pgtype.Timestamptz
	StateChange      pgtype.Timestamptz
	BackendXid       pgtype.Uint32
}

func (q *Queries) GetActivity(ctx context.Context) ([]GetActivityRow, error) {
	rows, err := q.db.Query(ctx, getActivity)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []GetActivityRow
	for rows.Next() {
		var i GetActivityRow
		if err := rows.Scan(
			&i.Pid,
			&i.State,
			&i.BlockedBy,
			&i.WaitEventType,
			&i.WaitEvent,
			&i.Query,
			&i.BackendType,
			&i.Datname,
			&i.Usename,
			&i.ApplicationName,
			&i.ClientAddr,
			&i.ClientHostname,
			&i.ClientPort,
			&i.CurrentTimestamp,
			&i.BackendStart,
			&i.XactStart,
			&i.QueryStart,
			&i.StateChange,
			&i.BackendXid,
		); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const getLocksForPids = `-- name: GetLocksForPids :many
SELECT l.pid, l.locktype, l.mode, l.granted, l.waitstart, l.relation,
    l.page, l.tuple, l.virtualxid, l.transactionid, l.classid,
    l.objid, l.objsubid, l.virtualtransaction,
    rel.relname, coalesce(rel.relkind, '')::text AS relkind
FROM pg_catalog.pg_locks l
LEFT JOIN pg_catalog.pg_class rel ON rel.oid=l.relation
WHERE l.pid=ANY($1::oid[])
`

type GetLocksForPidsRow struct {
	Pid                pgtype.Int4
	Locktype           pgtype.Text
	Mode               pgtype.Text
	Granted            pgtype.Bool
	Waitstart          pgtype.Timestamptz
	Relation           pgtype.Uint32
	Page               pgtype.Int4
	Tuple              pgtype.Int2
	Virtualxid         pgtype.Text
	Transactionid      pgtype.Uint32
	Classid            pgtype.Uint32
	Objid              pgtype.Uint32
	Objsubid           pgtype.Int2
	Virtualtransaction pgtype.Text
	Relname            pgtype.Text
	Relkind            string
}

func (q *Queries) GetLocksForPids(ctx context.Context, pids []pgtype.Uint32) ([]GetLocksForPidsRow, error) {
	rows, err := q.db.Query(ctx, getLocksForPids, pids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []GetLocksForPidsRow
	for rows.Next() {
		var i GetLocksForPidsRow
		if err := rows.Scan(
			&i.Pid,
			&i.Locktype,
			&i.Mode,
			&i.Granted,
			&i.Waitstart,
			&i.Relation,
			&i.Page,
			&i.Tuple,
			&i.Virtualxid,
			&i.Transactionid,
			&i.Classid,
			&i.Objid,
			&i.Objsubid,
			&i.Virtualtransaction,
			&i.Relname,
			&i.Relkind,
		); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}
