package pglocklogger

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
)

type PgLockLogger struct {
	options PgLockLoggerOptions
	conn    *pgx.Conn
}

type PgLockLoggerOptions struct {
	DSN      string
	Interval time.Duration
}

// Create a new PgLockLogger.
//
// You can use a PgLockLogger in two ways:
//
//  1. Call GetBlockedProcesses() whenever you want the information. In this
//     case, you must Close() the PgLockLogger when you don't use it anymore.
//  2. Set options.Interval and call Run() on it. The function passed to Run()
//     will be called when there are locks. Run() automatically calls Close().
func New(options PgLockLoggerOptions) *PgLockLogger {
	return &PgLockLogger{
		options: options,
	}
}

func (pg *PgLockLogger) Tx(ctx context.Context) (pgx.Tx, error) {
	if pg.conn != nil {
		if isReplica, err := pg.connectedToReplica(ctx); err != nil {
			pg.Close(context.WithoutCancel(ctx))
			return nil, err
		} else if isReplica {
			slog.InfoContext(ctx, "connected to replica - reconnecting to master")
			pg.Close(context.WithoutCancel(ctx))
		}
	}

	for pg.conn == nil {
		conn, err := pgx.Connect(ctx, pg.options.DSN)
		if err != nil {
			return nil, err
		}
		pg.conn = conn

		if isReplica, err := pg.connectedToReplica(ctx); err != nil {
			pg.Close(context.WithoutCancel(ctx))
			return nil, err
		} else if isReplica {
			slog.WarnContext(ctx, "connected to replica - reconnecting to master")
			pg.Close(context.WithoutCancel(ctx))
			time.Sleep(time.Second * 5)
		}
	}

	return pg.conn.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.RepeatableRead,
		AccessMode: pgx.ReadOnly,
	})
}

func (pg *PgLockLogger) Close(ctx context.Context) error {
	if pg.conn == nil {
		return nil
	}

	err := pg.conn.Close(ctx)
	pg.conn = nil
	return err
}

func (pg *PgLockLogger) connectedToReplica(ctx context.Context) (bool, error) {
	row := pg.conn.QueryRow(ctx, "SELECT pg_is_in_recovery();")
	var is_in_recovery bool
	err := row.Scan(&is_in_recovery)
	return is_in_recovery, err
}
