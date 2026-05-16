package postgres

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

const AnchorQueueLeaderLockID int64 = 340034

// AdvisoryLockLeaderElector holds a dedicated Postgres connection while this
// replica owns the anchor queue leadership lock.
type AdvisoryLockLeaderElector struct {
	store    *Store
	identity string
	lockID   int64
	interval time.Duration
	log      *zap.Logger

	leader atomic.Bool
	cancel context.CancelFunc
	done   chan struct{}
	once   sync.Once
}

func NewAdvisoryLockLeaderElector(store *Store, identity string, log *zap.Logger) *AdvisoryLockLeaderElector {
	if identity == "" {
		identity = "unknown"
	}
	return &AdvisoryLockLeaderElector{
		store:    store,
		identity: identity,
		lockID:   AnchorQueueLeaderLockID,
		interval: 2 * time.Second,
		log:      log,
		done:     make(chan struct{}),
	}
}

func (e *AdvisoryLockLeaderElector) Start(ctx context.Context) {
	runCtx, cancel := context.WithCancel(ctx)
	e.cancel = cancel
	go e.run(runCtx)
}

func (e *AdvisoryLockLeaderElector) Close() {
	if e.cancel == nil {
		return
	}
	e.cancel()
	<-e.done
}

func (e *AdvisoryLockLeaderElector) IsLeader(_ context.Context) bool {
	return e.leader.Load()
}

func (e *AdvisoryLockLeaderElector) Identity() string {
	return e.identity
}

func (e *AdvisoryLockLeaderElector) run(ctx context.Context) {
	defer close(e.done)
	ticker := time.NewTicker(e.interval)
	defer ticker.Stop()
	for {
		if e.tryHold(ctx) {
			return
		}
		select {
		case <-ctx.Done():
			e.leader.Store(false)
			return
		case <-ticker.C:
		}
	}
}

func (e *AdvisoryLockLeaderElector) tryHold(ctx context.Context) bool {
	conn, err := e.store.pool.Acquire(ctx)
	if err != nil {
		e.log.Warn("anchor leader connection unavailable", zap.Error(err))
		return false
	}
	var ok bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, e.lockID).Scan(&ok); err != nil {
		conn.Release()
		e.log.Warn("anchor leader lock attempt failed", zap.Error(err))
		return false
	}
	if !ok {
		conn.Release()
		e.leader.Store(false)
		return false
	}
	e.leader.Store(true)
	e.log.Info("anchor queue leadership acquired", zap.String("pod", e.identity))
	e.holdUntilDone(ctx, conn)
	return true
}

func (e *AdvisoryLockLeaderElector) holdUntilDone(ctx context.Context, conn *pgxpool.Conn) {
	defer conn.Release()
	<-ctx.Done()
	unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := conn.Exec(unlockCtx, `SELECT pg_advisory_unlock($1)`, e.lockID); err != nil {
		e.log.Warn("anchor leader unlock failed", zap.Error(err))
	}
	e.leader.Store(false)
}

func (e *AdvisoryLockLeaderElector) String() string {
	return fmt.Sprintf("postgres-advisory-lock:%d/%s", e.lockID, e.identity)
}
