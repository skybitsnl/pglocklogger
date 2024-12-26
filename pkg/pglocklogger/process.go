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
	BackendXid          uint32

	// Other backend processes this process is blocked on.
	BlockedBy []*BackendProcess

	// An overview of locks for this backend process. Locks that are not granted
	// are locks the process is currently blocked on.
	Locks []BackendProcessLocks
}

type BackendProcessLocks struct {
	// https://www.postgresql.org/docs/current/monitoring-stats.html#WAIT-EVENT-LOCK-TABLE
	Type      string
	Granted   bool
	Mode      string
	WaitStart time.Time

	// The following fields have a zero value if unset, or if the zero value would
	// be valid as well, the field is a ptr that is nil if unset.

	RelationOid  uint32
	RelationName string
	// r = ordinary table, i = index, S = sequence, t = TOAST table,
	// v = view, m = materialized view, c = composite type, f = foreign table,
	// p = partitioned table, I = partitioned index
	RelationKind string

	Page               *int32
	Tuple              *int16
	VirtualXid         string
	TransactionId      uint32
	ClassId            uint32
	ObjId              *uint32
	ObjSubId           *int16
	VirtualTransaction string
}

// For convenience, this returns true if the query itself isn't
// progressing because it is waiting for a lock.
func (bp BackendProcess) IsBlocked() bool {
	return bp.WaitEventType == "Lock"
}

func (p BackendProcess) String() string {
	sb := &strings.Builder{}
	fmt.Fprintf(sb, "Process %d (%s %q) is %s for %s (%s:%s)\n",
		p.Pid, p.BackendType, p.Application, p.State,
		p.CurrentDatabaseTime.Sub(p.StateChange),
		p.WaitEventType, p.WaitEvent)
	if p.Query == "" {
		fmt.Fprintf(sb, "  (no query)\n")
	} else {
		query := strings.ReplaceAll(strings.ReplaceAll(p.Query, "\r", ""), "\n", " ")
		if len(query) > 40 {
			query = query[0:40]
		}
		if p.State == "active" {
			fmt.Fprintf(sb, "  running for %s: %s\n",
				p.CurrentDatabaseTime.Sub(p.QueryStart),
				query,
			)
		} else {
			fmt.Fprintf(sb, "  last query started %s ago: %s\n",
				p.CurrentDatabaseTime.Sub(p.QueryStart),
				query,
			)
		}
	}
	if len(p.Locks) == 0 {
		fmt.Fprintf(sb, "  (holding no locks)\n")
	}

	const skipHoldingLockOnItself = true

	for _, lock := range p.Locks {
		var on []string

		if lock.RelationOid != 0 {
			switch lock.RelationKind {
			case "r":
				on = append(on, fmt.Sprintf("table %s", lock.RelationName))
			// TODO: if there's a lock on a table plus its indices, write "table X and 3 of its indices"
			case "i":
				on = append(on, fmt.Sprintf("index %s", lock.RelationName))
			case "S":
				on = append(on, fmt.Sprintf("sequence %s", lock.RelationName))
			case "t":
				on = append(on, fmt.Sprintf("TOAST table %s", lock.RelationName))
			case "v":
				on = append(on, fmt.Sprintf("view %s", lock.RelationName))
			case "m":
				on = append(on, fmt.Sprintf("materialized view %s", lock.RelationName))
			case "c":
				on = append(on, fmt.Sprintf("composite type %s", lock.RelationName))
			case "f":
				on = append(on, fmt.Sprintf("foreign table %s", lock.RelationName))
			case "p":
				on = append(on, fmt.Sprintf("partitioned table %s", lock.RelationName))
			case "I":
				on = append(on, fmt.Sprintf("partitioned index %s", lock.RelationName))
			case "":
				if lock.RelationName == "" {
					on = append(on, fmt.Sprintf("unknown OID %d", lock.RelationOid))
				} else {
					on = append(on, fmt.Sprintf("unknown OID %s (%d)", lock.RelationName, lock.RelationOid))
				}
			default:
				if lock.RelationName == "" {
					on = append(on, fmt.Sprintf("[%s] %d", lock.RelationKind, lock.RelationOid))
				} else {
					on = append(on, fmt.Sprintf("[%s] %s (%d)", lock.RelationKind, lock.RelationName, lock.RelationOid))
				}
			}
		}

		if lock.Page != nil {
			on = append(on, fmt.Sprintf("page %d", *lock.Page))
		}
		if lock.Tuple != nil {
			on = append(on, fmt.Sprintf("tuple %d", *lock.Tuple))
		}
		if lock.VirtualXid != "" {
			if lock.VirtualXid == lock.VirtualTransaction {
				if lock.Granted && skipHoldingLockOnItself {
					continue
				}
				on = append(on, fmt.Sprintf("itself (virtual XID %s)", lock.VirtualXid))
			} else {
				on = append(on, fmt.Sprintf("virtual XID %s", lock.VirtualXid))
			}
		}
		if lock.TransactionId != 0 {
			if lock.TransactionId == p.BackendXid {
				on = append(on, fmt.Sprintf("itself (XID %d)", p.BackendXid))
				if lock.Granted && skipHoldingLockOnItself {
					continue
				}
			} else {
				on = append(on, fmt.Sprintf("transaction XID %d", lock.TransactionId))
			}
		}
		if lock.ClassId != 0 {
			on = append(on, fmt.Sprintf("class id %d", lock.ClassId))
		}
		if lock.ObjId != nil {
			on = append(on, fmt.Sprintf("object id %d", *lock.ObjId))
		}
		if lock.ObjSubId != nil {
			on = append(on, fmt.Sprintf("object sub-id %d", *lock.ObjSubId))
		}

		if len(on) == 0 {
			on = append(on, "unknown target")
		}

		state := "waiting for"
		if lock.Granted {
			state = "holding"
		}
		fmt.Fprintf(sb, "  %s %s/%s lock", state, lock.Type, lock.Mode)

		fmt.Fprintf(sb, " on %s", strings.Join(on, ", "))
		if !lock.WaitStart.IsZero() {
			fmt.Fprintf(sb, " (since %s)", p.CurrentDatabaseTime.Sub(lock.WaitStart))
		}
		fmt.Fprintf(sb, "\n")
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
