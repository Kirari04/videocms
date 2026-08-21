package traffic

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	"ch/kirari04/videocms/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	DefaultFlushInterval  = 2 * time.Second
	DefaultFlushThreshold = 512
	DefaultFlushBatchSize = 5_000
	DefaultMaxPending     = 100_000
	defaultFlushTimeout   = 5 * time.Second
)

type Event struct {
	At               time.Time
	UserID           uint
	FileID           uint
	QualityID        uint
	AudioID          uint
	Source           string
	Bytes            uint64
	StoragePoolID    uint
	StorageMountUUID string
	DeliverySource   string
}

type Options struct {
	FlushInterval  time.Duration
	FlushThreshold int
	MaxPending     int
	Now            func() time.Time
	Logger         *log.Logger
}

type Status struct {
	PendingBuckets  int
	DroppedEvents   uint64
	FlushFailures   uint64
	FlushedRequests uint64
	LastFlushAt     *time.Time
	LastError       string
}

type aggregateKey struct {
	BucketStart      int64
	UserID           uint
	FileID           uint
	QualityID        uint
	AudioID          uint
	Source           string
	StoragePoolID    uint
	StorageMountUUID string
	DeliverySource   string
}

type aggregate struct {
	Bytes    uint64
	Requests uint64
}

type batchItem struct {
	Key      aggregateKey
	Identity string
}

type Recorder struct {
	db             *gorm.DB
	flushInterval  time.Duration
	flushThreshold int
	maxPending     int
	now            func() time.Time
	logger         *log.Logger

	mu              sync.Mutex
	pending         map[aggregateKey]aggregate
	inflight        int
	inflightKeys    map[aggregateKey]struct{}
	stopped         bool
	droppedEvents   uint64
	flushFailures   uint64
	flushedRequests uint64
	lastFlushAt     *time.Time
	lastError       string

	flushMu  sync.Mutex
	wake     chan struct{}
	stop     chan struct{}
	done     chan struct{}
	stopOnce sync.Once
}

func NewRecorder(db *gorm.DB, options Options) *Recorder {
	interval := options.FlushInterval
	if interval <= 0 {
		interval = DefaultFlushInterval
	}
	threshold := options.FlushThreshold
	if threshold <= 0 {
		threshold = DefaultFlushThreshold
	}
	maxPending := options.MaxPending
	if maxPending <= 0 {
		maxPending = DefaultMaxPending
	}
	now := options.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	logger := options.Logger
	if logger == nil {
		logger = log.Default()
	}
	recorder := &Recorder{
		db: db, flushInterval: interval, flushThreshold: threshold, maxPending: maxPending,
		now: now, logger: logger, pending: make(map[aggregateKey]aggregate),
		inflightKeys: make(map[aggregateKey]struct{}),
		wake:         make(chan struct{}, 1), stop: make(chan struct{}), done: make(chan struct{}),
	}
	go recorder.run()
	return recorder
}

func (r *Recorder) Record(event Event) {
	if r == nil || r.db == nil || event.Bytes == 0 {
		return
	}
	at := event.At
	if at.IsZero() {
		at = r.now()
	}
	key := aggregateKey{
		BucketStart: at.UTC().Truncate(time.Minute).Unix(), UserID: event.UserID, FileID: event.FileID,
		QualityID: event.QualityID, AudioID: event.AudioID, Source: event.Source,
		StoragePoolID: event.StoragePoolID, StorageMountUUID: event.StorageMountUUID,
		DeliverySource: event.DeliverySource,
	}

	r.mu.Lock()
	current, exists := r.pending[key]
	_, currentlyFlushing := r.inflightKeys[key]
	if r.stopped || (!exists && !currentlyFlushing && len(r.pending)+r.inflight >= r.maxPending) {
		r.droppedEvents++
		dropped := r.droppedEvents
		r.mu.Unlock()
		if dropped == 1 || dropped%1000 == 0 {
			r.logger.Printf("component=traffic_recorder event=event_dropped total=%d", dropped)
		}
		return
	}
	current.Bytes += event.Bytes
	current.Requests++
	r.pending[key] = current
	shouldWake := len(r.pending) >= r.flushThreshold
	r.mu.Unlock()
	if shouldWake {
		r.signal()
	}
}

func (r *Recorder) Status() Status {
	if r == nil {
		return Status{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	status := Status{
		PendingBuckets: len(r.pending) + r.inflight, DroppedEvents: r.droppedEvents,
		FlushFailures: r.flushFailures, FlushedRequests: r.flushedRequests, LastError: r.lastError,
	}
	if r.lastFlushAt != nil {
		value := *r.lastFlushAt
		status.LastFlushAt = &value
	}
	return status
}

func (r *Recorder) Flush(ctx context.Context) error {
	for {
		remaining, err := r.flushBatch(ctx)
		if err != nil {
			return err
		}
		if !remaining {
			return nil
		}
	}
}

func (r *Recorder) flushBatch(ctx context.Context) (bool, error) {
	if r == nil || r.db == nil {
		return false, nil
	}
	r.flushMu.Lock()
	defer r.flushMu.Unlock()

	r.mu.Lock()
	if len(r.pending) == 0 {
		r.mu.Unlock()
		return false, nil
	}
	batch := make(map[aggregateKey]aggregate, min(len(r.pending), DefaultFlushBatchSize))
	for key, value := range r.pending {
		batch[key] = value
		delete(r.pending, key)
		if len(batch) == DefaultFlushBatchSize {
			break
		}
	}
	r.inflight = len(batch)
	r.inflightKeys = make(map[aggregateKey]struct{}, len(batch))
	for key := range batch {
		r.inflightKeys[key] = struct{}{}
	}
	r.mu.Unlock()

	items := make([]batchItem, 0, len(batch))
	for key := range batch {
		items = append(items, batchItem{Key: key, Identity: rollupIdentity(key)})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Identity < items[j].Identity })
	now := r.now().UTC()
	rows := make([]models.TrafficLog, 0, len(items))
	var requests uint64
	for _, item := range items {
		key := item.Key
		value := batch[key]
		bucket := time.Unix(key.BucketStart, 0).UTC()
		identity := item.Identity
		requests += value.Requests
		rows = append(rows, models.TrafficLog{
			Model:  models.Model{CreatedAt: &bucket, UpdatedAt: &now},
			UserID: key.UserID, FileID: key.FileID, QualityID: key.QualityID, AudioID: key.AudioID,
			Source: key.Source, Bytes: value.Bytes, RequestCount: value.Requests,
			BucketStart: key.BucketStart, RollupKey: &identity, StoragePoolID: key.StoragePoolID,
			StorageMountUUID: key.StorageMountUUID, DeliverySource: key.DeliverySource,
		})
	}

	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "rollup_key"}},
			DoUpdates: clause.Assignments(map[string]any{
				"bytes":         gorm.Expr("traffic_logs.bytes + excluded.bytes"),
				"request_count": gorm.Expr("traffic_logs.request_count + excluded.request_count"),
				"updated_at":    now,
			}),
		}).CreateInBatches(&rows, 250).Error
	})

	r.mu.Lock()
	r.inflight = 0
	r.inflightKeys = make(map[aggregateKey]struct{})
	if err != nil {
		for key, value := range batch {
			current := r.pending[key]
			current.Bytes += value.Bytes
			current.Requests += value.Requests
			r.pending[key] = current
		}
		r.flushFailures++
		r.lastError = err.Error()
		failures := r.flushFailures
		r.mu.Unlock()
		if failures == 1 || failures%10 == 0 {
			r.logger.Printf("component=traffic_recorder event=flush_failed pending=%d failures=%d error=%q", len(batch), failures, err)
		}
		return true, err
	}
	r.flushedRequests += requests
	r.lastFlushAt = &now
	r.lastError = ""
	hasPending := len(r.pending) > 0
	r.mu.Unlock()
	return hasPending, nil
}

func (r *Recorder) Shutdown(ctx context.Context) error {
	if r == nil {
		return nil
	}
	r.stopOnce.Do(func() {
		r.mu.Lock()
		r.stopped = true
		r.mu.Unlock()
		close(r.stop)
	})
	select {
	case <-r.done:
	case <-ctx.Done():
		return ctx.Err()
	}
	return r.Flush(ctx)
}

func (r *Recorder) run() {
	defer close(r.done)
	ticker := time.NewTicker(r.flushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			r.flushWithTimeout()
		case <-r.wake:
			r.flushWithTimeout()
		case <-r.stop:
			return
		}
	}
}

func (r *Recorder) flushWithTimeout() {
	ctx, cancel := context.WithTimeout(context.Background(), defaultFlushTimeout)
	defer cancel()
	remaining, _ := r.flushBatch(ctx)
	if remaining {
		r.signal()
	}
}

func (r *Recorder) signal() {
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

func rollupIdentity(key aggregateKey) string {
	value := fmt.Sprintf("%d\x00%d\x00%d\x00%d\x00%d\x00%s\x00%d\x00%s\x00%s",
		key.BucketStart, key.UserID, key.FileID, key.QualityID, key.AudioID, key.Source,
		key.StoragePoolID, key.StorageMountUUID, key.DeliverySource)
	digest := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%x", digest[:])
}
