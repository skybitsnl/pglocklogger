package pglocklogger

import (
	"context"
	"time"
)

func (pg *PgLockLogger) Run(ctx context.Context, f func(p BackendProcess)) error {
	defer pg.Close(context.WithoutCancel(ctx))

	ticker := time.NewTicker(pg.options.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			ps, err := pg.GetBlockedProcesses(ctx)
			if err != nil {
				return err
			}
			for _, p := range ps {
				f(p)
			}
		}
	}
}
