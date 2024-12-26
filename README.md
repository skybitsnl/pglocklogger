# pglocklogger

*If you're here because your application is broken right now and you suspect
PostgreSQL locks may be the problem, see the "Emergency" section at the bottom
of this document.*

Perhaps you've had locking issues recently and you're investigating how to solve
or prevent those issues in the future. If so, you've come to the right place!

PostgreSQL maintains excellent in-memory information about its current
activities. It also has logging for when queries are slow or blocked on locks
for a long time. However, it can be hard to figure out from the logging what
exactly was the state of the activities at the time.

This tool helps with that. When there are transactions blocked on locks, this
tool prints live information about the blocked transaction, the transaction
it is blocked on, the rest of the wait queue, and what all those transactions
are trying to do.

## Quick start

There's three easy ways to run the tool:

1. Use the Docker image using
   `docker run sjorsgielen/pglocklogger:latest -dsn ...`. Or, build it yourself
   using `goreleaser release --snapshot --clean`.
2. Run the tool directly from the repo using `go run ./cmd -dsn ...` (requires
   a Go toolchain)
3. Build the tool using `go build -o pglocklogger ./cmd` (requires a Go
   toolchain, you can also build for other architectures), then run it at your
   target location using `./pglocklogger -dsn ...`

Example output:

```
$ ./pglocklogger -dsn ...
2024/12/25 14:01:03 Process 4423 (psql/client backend) is active for 21m9.917198s (Lock:relation)
  waiting for relation/AccessShareLock lock on r/test (since 21m9.91618s)
  holding virtualxid/ExclusiveLock lock on /
  blocked by 1 processses:
  - Process 4421 (psql/client backend) is active for 21m17.883074s (Lock:relation)
      waiting for relation/AccessExclusiveLock lock on r/test (since 21m17.880427s)
      holding transactionid/ExclusiveLock lock on /
      holding virtualxid/ExclusiveLock lock on /
      blocked by 1 processses:
      - Process 4419 (psql/client backend) is idle in transaction for 21m39.917687s (Client:ClientRead)
          holding relation/RowShareLock lock on r/test
          holding transactionid/ExclusiveLock lock on /
          holding virtualxid/ExclusiveLock lock on /
```

## Advice for migrations & further reading

When you perform 'zero-downtime' schema migrations, and you run into locking
issues during them, then make sure you set an appropriate `lock_timeout`. See
[this article](https://postgres.ai/blog/20210923-zero-downtime-postgres-schema-migrations-lock-timeout-and-retries)
for more details and alternatives.

The following articles and resources were very helpful writing this tool,
and I would suggest them for further reading:

- [One PID to Lock Them All: Finding the Source of the Lock in Postgres](https://www.crunchydata.com/blog/one-pid-to-lock-them-all-finding-the-source-of-the-lock-in-postgres)
- [Zero-downtime Postgres schema migrations need this: lock_timeout and retries](https://postgres.ai/blog/20210923-zero-downtime-postgres-schema-migrations-lock-timeout-and-retries)
- [Postgres Log Monitoring 101: Deadlocks, Checkpoint Tuning & Blocked Queries](https://pganalyze.com/blog/postgresql-log-monitoring-101-deadlocks-checkpoints-blocked-queries)
- [Lock Monitoring (Postgres Wiki)](https://wiki.postgresql.org/wiki/Lock_Monitoring)
- [Chapter 27. Monitoring Database Activity](https://www.postgresql.org/docs/current/monitoring.html)

## Emergency

Run the following query to quickly find out what's blocking what. Especially watch rows with
the lowest lock depth (ending in `.0`).

```
WITH sos AS (
	SELECT array_cat(array_agg(pid),
           array_agg((pg_blocking_pids(pid))[array_length(pg_blocking_pids(pid),1)])) pids
	FROM pg_locks
	WHERE NOT granted
)
SELECT a.pid, a.usename, a.datname, a.state,
	   a.wait_event_type || ': ' || a.wait_event AS wait_event,
       current_timestamp-a.state_change time_in_state,
       current_timestamp-a.xact_start time_in_xact,
       l.relation::regclass relname,
       l.locktype, l.mode, l.page, l.tuple,
       pg_blocking_pids(l.pid) blocking_pids,
       (pg_blocking_pids(l.pid))[array_length(pg_blocking_pids(l.pid),1)] last_session,
       coalesce((pg_blocking_pids(l.pid))[1]||'.'||coalesce(case when locktype='transactionid' then 1 else array_length(pg_blocking_pids(l.pid),1)+1 end,0),a.pid||'.0') lock_depth,
       a.query
FROM pg_stat_activity a
     JOIN sos s on (a.pid = any(s.pids))
     LEFT OUTER JOIN pg_locks l on (a.pid = l.pid and not l.granted)
ORDER BY lock_depth;
```

Record the output for future reference if you wish.

Once you know which row is most likely causing the issue, cancel that transaction either
through normal means in your application, or by cancelling or terminating the PostgreSQL
backend:

```
SELECT pg_cancel_backend(PID);
-- or
SELECT pg_terminate_backend(PID);
```

Repeat as necessary.

These queries come from the articles in the previous section. For further
reading, refer to there.
