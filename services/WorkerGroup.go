package services

import (
	"ch/kirari04/videocms/app"
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/logic"
	"context"
	"sync"
	"time"
)

type WorkerGroup struct {
	deps  *app.Deps
	logic *logic.Service

	activeEncodingsMu sync.Mutex
	activeEncodings   []ActiveEncoding
	limitChan         chan bool

	activeDownloadsMu     sync.Mutex
	activeDownloadCancels map[uint]context.CancelFunc

	resourcesInterval time.Duration
	netSent           uint64
	netRecv           uint64
	diskWrite         uint64
	diskRead          uint64
}

func NewWorkerGroup(deps *app.Deps, logicSvc *logic.Service) *WorkerGroup {
	if logicSvc == nil && deps != nil {
		logicSvc = logic.NewService(deps)
	}
	return &WorkerGroup{
		deps:                  deps,
		logic:                 logicSvc,
		activeDownloadCancels: map[uint]context.CancelFunc{},
		resourcesInterval:     time.Second * 10,
	}
}

func (w *WorkerGroup) Config() config.Config {
	return w.deps.Config()
}

func (w *WorkerGroup) Start(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}

	cfg := w.Config()
	if cfg.EncodingEnabled != nil && *cfg.EncodingEnabled {
		w.ResetEncodingState()
		go w.Encoder(ctx)
	}

	go w.Downloader(ctx)
	go w.EncoderCleanup(ctx)
	go w.Deleter(ctx)
	go w.AuditCleanup(ctx)
	go w.Resources(ctx)
}

func sleepContext(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
