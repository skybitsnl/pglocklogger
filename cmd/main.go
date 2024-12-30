package main

import (
	"context"
	"flag"
	"log"
	"time"

	"github.com/skybitsnl/pglocklogger/pkg/pglocklogger"
)

var (
	dsn                 = flag.String("dsn", "", "DSN of the database")
	interval            = flag.Duration("interval", time.Second, "Interval of lock retrieval")
	min_active_duration = flag.Duration("min-active-duration", time.Millisecond*100, "Minimum time for a process to be active for it to be reported")
)

func main() {
	flag.Parse()

	if *dsn == "" {
		log.Fatal("missing flags, see -help")
	}

	pglock := pglocklogger.New(pglocklogger.PgLockLoggerOptions{
		DSN:               *dsn,
		Interval:          *interval,
		MinActiveDuration: *min_active_duration,
	})
	err := pglock.Run(context.Background(), func(p pglocklogger.BackendProcess) {
		log.Print(p)
	})
	log.Fatalf("Run() returned with: %+v", err)
}
