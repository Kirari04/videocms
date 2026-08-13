package services

import (
	"ch/kirari04/videocms/app"
	"ch/kirari04/videocms/config"
	downloadsvc "ch/kirari04/videocms/download"
	"ch/kirari04/videocms/logic"
	"context"
	"log"
	"sync"
	"time"
)

type WorkerGroup struct {
	deps  *app.Deps
	logic *logic.Service

	activeEncodingsMu sync.Mutex
	activeEncodings   []ActiveEncoding

	encoderMu            sync.Mutex
	activeEncodingJobs   int
	encoderConfigChanged chan struct{}
	encoderPollInterval  time.Duration
	encoderLogger        *log.Logger
	encodingTaskRunner   func(context.Context, EncodingTask) error

	activeDownloadsMu     sync.Mutex
	activeDownloadCancels map[uint]context.CancelFunc

	activePreparationsMu sync.Mutex
	activePreparations   map[uint]activeDownloadPreparation
	downloadAssembler    downloadsvc.Assembler
	preparationTimeout   func(float64) time.Duration

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
		activePreparations:    map[uint]activeDownloadPreparation{},
		downloadAssembler:     downloadsvc.FFmpegAssembler{},
		preparationTimeout:    downloadPreparationTimeout,
		encoderConfigChanged:  make(chan struct{}, 1),
		encoderPollInterval:   time.Second * 10,
		encoderLogger:         log.Default(),
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

	// Reset jobs left in progress by a previous process before starting the
	// scheduler. The scheduler stays alive while encoding is disabled so an
	// administrator can enable it without restarting the application.
	w.ResetEncodingState()
	go w.Encoder(ctx)

	go w.Downloader(ctx)
	go w.DownloadPreparer(ctx)
	go w.DownloadPreparationCleanup(ctx)
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
