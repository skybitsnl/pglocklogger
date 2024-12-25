package pglocklogger

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/netip"
	"time"
)

// Retrieve blocked processes from PostgreSQL.
//
// The main table queried is pg_stat_activity, which contains all ongoing processes.
// We take all processes from this table that are waiting for something. From those
// rows, we join information from pg_locks and other pg_stat_activity rows to get
// more information about what the process is waiting for, and why.
func (pg *PgLockLogger) GetBlockedProcesses(ctx context.Context) ([]BackendProcess, error) {
	if err := pg.Connect(ctx); err != nil {
		return nil, err
	}

	// Start with all activity
	query := `
		SELECT
			activity.*,
			pg_blocking_pids(activity.pid) as blocked_by
		FROM pg_stat_activity activity
	`

	// In a recursive query, request all activity blocked on a lock,
	// plus the PIDs they are blocked on
	query = `
		WITH RECURSIVE activities AS (
			` + query + `
			WHERE wait_event_type='Lock'
			UNION
			` + query + `, activities
			WHERE activity.pid=any(activities.blocked_by)
		)`

	// Enrich the results with locks held and awaiting by all activities
	// returned
	query = query + `
		SELECT
			pid, state, blocked_by, wait_event_type, wait_event,
			query, backend_type, datname, usename, application_name,
			client_addr, client_hostname, client_port, current_timestamp,
			backend_start, xact_start, query_start, state_change,
			backend_xid,
			(
				SELECT coalesce(
					jsonb_agg(
						jsonb_build_object(
							'lock',
							row_to_json(locks),
							'rel',
							jsonb_build_object(
								'name', rel.relname,
								'kind', rel.relkind
							)
						)
					),
					'[]'
				)
				FROM pg_locks locks
				LEFT JOIN pg_class rel ON rel.oid=locks.relation
				WHERE locks.pid=activities.pid
			) AS locks
		FROM activities
		`

	type ActivityLockL struct {
		// https://www.postgresql.org/docs/current/monitoring-stats.html#WAIT-EVENT-LOCK-TABLE
		Type      string    `json:"locktype"`
		Mode      string    `json:"mode"`
		Granted   bool      `json:"granted"`
		WaitStart time.Time `json:"waitstart"`

		// Where does the lock point to?
		// https://www.postgresql.org/docs/current/view-pg-locks.html
		RelationOid        *string `json:"relation,omitempty"`
		Page               *int    `json:"page,omitempty"`
		Tuple              *int    `json:"tuple,omitempty"`
		VirtualXid         *string `json:"virtualxid,omitempty"`
		TransactionId      *string `json:"transactionid,omitempty"`
		ClassId            *string `json:"classid,omitempty"`
		ObjId              *string `json:"objid,omitempty"`
		ObjSubId           *int    `json:"objsubid,omitempty"`
		VirtualTransaction *string `json:"virtualtransaction,omitempty"`
	}

	type ActivityLockR struct {
		Name string `json:"name"`
		// r = ordinary table, i = index, S = sequence, t = TOAST table,
		// v = view, m = materialized view, c = composite type, f = foreign table,
		// p = partitioned table, I = partitioned index
		Kind string `json:"kind"`
	}

	type ActivityLock struct {
		Lock ActivityLockL `json:"lock"`
		Rel  ActivityLockR `json:"rel"`
	}

	type ActivityRow struct {
		Pid           int64
		State         string
		BlockedByPids []int64
		WaitEventType sql.NullString
		WaitEvent     sql.NullString
		Query         string
		BackendType   string

		Database            string
		Username            sql.NullString
		Application         string
		ClientAddress       netip.Addr
		ClientHostname      sql.NullString
		ClientPort          int
		CurrentDatabaseTime time.Time
		BackendStart        time.Time
		TransactionStart    time.Time
		QueryStart          time.Time
		StateChange         time.Time
		BackendXid          sql.NullString

		LockBytes []byte
		Locks     []ActivityLock
	}

	rows, err := pg.conn.Query(ctx, query)
	if err != nil {
		// TODO: if the query fails with a network error, just back-off and try to reconnect
		return nil, err
	}
	defer rows.Close()

	activities := map[int64]ActivityRow{}
	processes := map[int64]*BackendProcess{}

	for rows.Next() {
		var row ActivityRow
		if err := rows.Scan(
			&row.Pid, &row.State, &row.BlockedByPids, &row.WaitEventType,
			&row.WaitEvent, &row.Query, &row.BackendType, &row.Database,
			&row.Username, &row.Application, &row.ClientAddress, &row.ClientHostname,
			&row.ClientPort, &row.CurrentDatabaseTime, &row.BackendStart, &row.TransactionStart,
			&row.QueryStart, &row.StateChange, &row.BackendXid, &row.LockBytes); err != nil {
			return nil, err
		}

		if _, ok := activities[row.Pid]; ok {
			panic("the same activity was returned multiple times")
		}

		if err := json.Unmarshal(row.LockBytes, &row.Locks); err != nil {
			slog.WarnContext(ctx, "JSON unmarshal failed",
				slog.String("bytes", string(row.LockBytes)),
				slog.String("err", err.Error()),
			)
			return nil, err
		}

		activities[row.Pid] = row
		processes[row.Pid] = &BackendProcess{
			Pid:                 row.Pid,
			State:               row.State,
			WaitEventType:       row.WaitEventType.String,
			WaitEvent:           row.WaitEvent.String,
			BackendType:         row.BackendType,
			Query:               row.Query,
			Database:            row.Database,
			Username:            row.Username.String,
			Application:         row.Application,
			ClientAddress:       row.ClientAddress,
			ClientHostname:      row.ClientHostname.String,
			ClientPort:          row.ClientPort,
			CurrentDatabaseTime: row.CurrentDatabaseTime,
			BackendStart:        row.BackendStart,
			TransactionStart:    row.TransactionStart,
			QueryStart:          row.QueryStart,
			StateChange:         row.StateChange,
			BackendXid:          row.BackendXid.String,
		}
	}

	for pid, process := range processes {
		activity := activities[pid]
		for _, blockerPid := range activity.BlockedByPids {
			process.BlockedBy = append(process.BlockedBy, processes[blockerPid])
		}

		for _, lock := range activity.Locks {
			pLock := BackendProcessLocks{
				Type:      lock.Lock.Type,
				Granted:   lock.Lock.Granted,
				Mode:      lock.Lock.Mode,
				WaitStart: lock.Lock.WaitStart,

				RelationOid:        unref(lock.Lock.RelationOid),
				RelationName:       lock.Rel.Name,
				RelationKind:       lock.Rel.Kind,
				Page:               lock.Lock.Page,
				Tuple:              lock.Lock.Tuple,
				VirtualXid:         unref(lock.Lock.VirtualXid),
				TransactionId:      unref(lock.Lock.TransactionId),
				ClassId:            unref(lock.Lock.ClassId),
				ObjId:              unref(lock.Lock.ObjId),
				ObjSubId:           lock.Lock.ObjSubId,
				VirtualTransaction: unref(lock.Lock.VirtualTransaction),
			}

			process.Locks = append(process.Locks, pLock)
		}
	}

	var res []BackendProcess
	for _, process := range processes {
		if !process.IsBlocked() {
			continue
		}

		res = append(res, *process)
	}

	return res, nil
}

func unref[T any](ptr *T) T {
	if ptr == nil {
		var zero T
		return zero
	} else {
		return *ptr
	}
}
