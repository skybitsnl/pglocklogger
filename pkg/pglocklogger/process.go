package pglocklogger

import (
	"bufio"
	"fmt"
	"net/netip"
	"strings"
	"time"
)

// A BackendProcess represents a PostgreSQL backend process (typically, a
// transaction).
type BackendProcess struct {
	Pid int64
	// states returned by PostgreSQL:
	// - idle
	// - idle in transaction
	// - active (a query is ongoing)
	// - (empty, for background processes)
	State string
	// wait event types returned by PostgreSQL:
	// - Activity (background processes)
	// - Client (waiting for client to send a new query)
	// - Lock (waiting for a lock to be acquired)
	WaitEventType string
	WaitEvent     string
	BackendType   string
	// if this contains "<insufficient privilege>", try again with a database
	// super-user
	Query string

	Database       string
	Username       string
	Application    string
	ClientAddress  netip.Addr
	ClientHostname string
	// uint16, or -1 if UNIX socket
	ClientPort          int
	CurrentDatabaseTime time.Time
	BackendStart        time.Time
	TransactionStart    time.Time
	QueryStart          time.Time
	StateChange         time.Time

	// Other backend processes this process is blocked on.
	BlockedBy []*BackendProcess

	// An overview of locks for this backend process. Locks that are not granted
	// are locks the process is currently blocked on.
	Locks []BackendProcessLocks
}

type BackendProcessLocks struct {
	// https://www.postgresql.org/docs/current/monitoring-stats.html#WAIT-EVENT-LOCK-TABLE
	Type         string
	Granted      bool
	Mode         string
	WaitStart    time.Time
	RelationOid  string
	RelationName string
	// r = ordinary table, i = index, S = sequence, t = TOAST table,
	// v = view, m = materialized view, c = composite type, f = foreign table,
	// p = partitioned table, I = partitioned index
	RelationKind string
}

// For convenience, this returns true if the query itself isn't
// progressing because it is waiting for a lock.
func (bp BackendProcess) IsBlocked() bool {
	return bp.WaitEventType == "Lock"
}

func (p BackendProcess) String() string {
	sb := &strings.Builder{}
	fmt.Fprintf(sb, "Process %d (%s/%s) is %s for %s (%s:%s)\n",
		p.Pid, p.Application, p.BackendType, p.State,
		p.CurrentDatabaseTime.Sub(p.StateChange),
		p.WaitEventType, p.WaitEvent)
	if len(p.Locks) == 0 {
		fmt.Fprintf(sb, "  (holding no locks)\n")
	}
	for _, lock := range p.Locks {
		state := "waiting for"
		if lock.Granted {
			state = "holding"
		}
		since := ""
		if !lock.WaitStart.IsZero() {
			since = fmt.Sprintf(" (since %s)", p.CurrentDatabaseTime.Sub(lock.WaitStart))
		}
		fmt.Fprintf(sb, "  %s %s/%s lock on %s/%s%s\n",
			state, lock.Type, lock.Mode, lock.RelationKind, lock.RelationName,
			since,
		)
	}

	if len(p.BlockedBy) == 0 {
		fmt.Fprintf(sb, "  (blocked by no processes)\n")
	} else {
		fmt.Fprintf(sb, "  blocked by %d processses:\n", len(p.BlockedBy))
	}
	for _, blocker := range p.BlockedBy {
		scanner := bufio.NewScanner(strings.NewReader(blocker.String()))
		begin := "- "
		for scanner.Scan() {
			fmt.Fprintf(sb, "  %s%s\n", begin, scanner.Text())
			begin = "  "
		}
	}
	return sb.String()
}
