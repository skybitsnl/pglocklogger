package pglocklogger

import (
	"context"
	"slices"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/skybitsnl/pglocklogger/pkg/pglocklogger/gendb"
)

// Retrieve blocked processes from PostgreSQL.
//
// The main table queried is pg_stat_activity, which contains all ongoing processes.
// We take all processes from this table that are waiting for something. From those
// rows, we join information from pg_locks and other pg_stat_activity rows to get
// more information about what the process is waiting for, and why.
func (pg *PgLockLogger) GetBlockedProcesses(ctx context.Context) ([]BackendProcess, error) {
	tx, err := pg.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))

	q := gendb.New(tx)

	activityRows, err := q.GetActivity(ctx)
	if err != nil {
		return nil, err
	}

	if len(activityRows) == 0 {
		return nil, nil
	}

	var pids []pgtype.Uint32
	activities := map[int32]gendb.GetActivityRow{}
	processes := map[int32]*BackendProcess{}

	for _, row := range activityRows {
		if _, ok := activities[row.Pid.Int32]; ok {
			panic("the same activity was returned multiple times")
		}

		activities[row.Pid.Int32] = row
		processes[row.Pid.Int32] = &BackendProcess{
			Pid:                 int64(row.Pid.Int32),
			State:               row.State.String,
			WaitEventType:       row.WaitEventType.String,
			WaitEvent:           row.WaitEvent.String,
			BackendType:         row.BackendType.String,
			Query:               row.Query.String,
			Database:            row.Datname.String,
			Username:            row.Usename.String,
			Application:         row.ApplicationName.String,
			ClientAddress:       unref(row.ClientAddr),
			ClientHostname:      row.ClientHostname.String,
			ClientPort:          int(row.ClientPort.Int32),
			CurrentDatabaseTime: row.CurrentTimestamp.Time,
			BackendStart:        row.BackendStart.Time,
			TransactionStart:    row.XactStart.Time,
			QueryStart:          row.QueryStart.Time,
			StateChange:         row.StateChange.Time,
			BackendXid:          row.BackendXid.Uint32,
		}
		pids = append(pids, pgtype.Uint32{Uint32: uint32(row.Pid.Int32), Valid: true})
	}

	lockRows, err := q.GetLocksForPids(ctx, pids)
	if err != nil {
		return nil, err
	}

	for pid, process := range processes {
		activity := activities[pid]
		for _, blockerPid := range activity.BlockedBy {
			process.BlockedBy = append(process.BlockedBy, processes[blockerPid])
		}

		for _, lock := range lockRows {
			if lock.Pid.Int32 != pid {
				continue
			}
			pLock := BackendProcessLocks{
				Type:      lock.Locktype.String,
				Granted:   lock.Granted.Bool,
				Mode:      lock.Mode.String,
				WaitStart: lock.Waitstart.Time,

				RelationOid:        lock.Relation.Uint32,
				RelationName:       lock.Relname.String,
				RelationKind:       lock.Relkind,
				Page:               ref(lock.Page.Valid, lock.Page.Int32),
				Tuple:              ref(lock.Tuple.Valid, lock.Tuple.Int16),
				VirtualXid:         lock.Virtualxid.String,
				TransactionId:      lock.Transactionid.Uint32,
				ClassId:            lock.Classid.Uint32,
				ObjId:              ref(lock.Objid.Valid, lock.Objid.Uint32),
				ObjSubId:           ref(lock.Objsubid.Valid, lock.Objsubid.Int16),
				VirtualTransaction: lock.Virtualtransaction.String,
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

	slices.SortFunc(res, func(a, b BackendProcess) int {
		return a.StateChange.Compare(b.StateChange)
	})

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

func ref[T any](valid bool, t T) *T {
	if valid {
		return &t
	} else {
		return nil
	}
}
