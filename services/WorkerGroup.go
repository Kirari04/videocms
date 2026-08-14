package services

import (
	"ch/kirari04/videocms/app"
	"ch/kirari04/videocms/config"
	downloadsvc "ch/kirari04/videocms/download"
	"ch/kirari04/videocms/logic"
	"context"
	"fmt"
	"log"
	"runtime/debug"
	"sort"
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

	serviceHealthMu sync.Mutex
	serviceHealth   map[string]ServiceHealth
}

type ServiceHealth struct {
	Name        string     `json:"name"`
	Status      string     `json:"status"`
	Restarts    int        `json:"restarts"`
	LastStartAt *time.Time `json:"lastStartAt,omitempty"`
	LastError   string     `json:"lastError,omitempty"`
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
		serviceHealth:         map[string]ServiceHealth{},
	}
}

func (w *WorkerGroup) Config() config.Config {
	return w.deps.Config()
}

func (w *WorkerGroup) Start(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}

	// Finite work is owned by the durable background runtime. Only continuous
	// samplers remain supervised services.
	go w.supervise(ctx, "resource-sampler", w.Resources)
}

func (w *WorkerGroup) supervise(ctx context.Context, name string, service func(context.Context)) {
	backoffs := []time.Duration{time.Second, 5 * time.Second, 30 * time.Second, time.Minute}
	restarts := 0
	for ctx.Err() == nil {
		now := time.Now()
		w.setServiceHealth(ServiceHealth{Name: name, Status: "running", Restarts: restarts, LastStartAt: &now})
		panicMessage := runSupervised(ctx, service)
		if ctx.Err() != nil {
			w.setServiceHealth(ServiceHealth{Name: name, Status: "stopped", Restarts: restarts, LastStartAt: &now})
			return
		}
		restarts++
		if panicMessage == "" {
			panicMessage = "service stopped unexpectedly"
		}
		w.setServiceHealth(ServiceHealth{Name: name, Status: "degraded", Restarts: restarts, LastStartAt: &now, LastError: panicMessage})
		delay := backoffs[len(backoffs)-1]
		if restarts-1 < len(backoffs) {
			delay = backoffs[restarts-1]
		}
		if !sleepContext(ctx, delay) {
			return
		}
	}
}

func runSupervised(ctx context.Context, service func(context.Context)) (panicMessage string) {
	defer func() {
		if recovered := recover(); recovered != nil {
			panicMessage = fmt.Sprintf("panic: %v\n%s", recovered, debug.Stack())
		}
	}()
	service(ctx)
	return ""
}

func (w *WorkerGroup) setServiceHealth(health ServiceHealth) {
	w.serviceHealthMu.Lock()
	w.serviceHealth[health.Name] = health
	w.serviceHealthMu.Unlock()
}

func (w *WorkerGroup) ServiceHealth() []ServiceHealth {
	w.serviceHealthMu.Lock()
	defer w.serviceHealthMu.Unlock()
	result := make([]ServiceHealth, 0, len(w.serviceHealth))
	for _, health := range w.serviceHealth {
		result = append(result, health)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
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
