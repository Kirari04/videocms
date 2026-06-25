package app

import (
	"sync"
	"sync/atomic"

	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/models"

	"github.com/patrickmn/go-cache"
	"gorm.io/gorm"
)

type Snapshot struct {
	Config    config.Config
	Qualities []models.AvailableQuality
}

type SnapshotStore struct {
	value atomic.Value
}

func NewSnapshotStore(snapshot Snapshot) *SnapshotStore {
	store := &SnapshotStore{}
	store.Replace(snapshot)
	return store
}

func (s *SnapshotStore) Current() Snapshot {
	value := s.value.Load()
	if value == nil {
		return Snapshot{}
	}
	return value.(Snapshot)
}

func (s *SnapshotStore) Config() config.Config {
	return s.Current().Config
}

func (s *SnapshotStore) Qualities() []models.AvailableQuality {
	qualities := s.Current().Qualities
	out := make([]models.AvailableQuality, len(qualities))
	copy(out, qualities)
	return out
}

func (s *SnapshotStore) Replace(snapshot Snapshot) {
	snapshot.Config = snapshot.Config.Clone()
	qualities := make([]models.AvailableQuality, len(snapshot.Qualities))
	copy(qualities, snapshot.Qualities)
	snapshot.Qualities = qualities
	s.value.Store(snapshot)
}

type Deps struct {
	DB          *gorm.DB
	Snapshots   *SnapshotStore
	Cache       *cache.Cache
	RequestGate *RequestGate
}

func (d *Deps) Config() config.Config {
	return d.Snapshots.Config()
}

func (d *Deps) Qualities() []models.AvailableQuality {
	return d.Snapshots.Qualities()
}

type RequestGate struct {
	mu     sync.Mutex
	active map[uint]bool
	block  bool
}

func NewRequestGate() *RequestGate {
	return &RequestGate{active: map[uint]bool{}}
}

func (g *RequestGate) Blocked(userID uint) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.block || g.active[userID]
}

func (g *RequestGate) Start(userID uint) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.active[userID] = true
}

func (g *RequestGate) End(userID uint) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.active, userID)
}

func (g *RequestGate) Sync(force bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if force || g.active == nil {
		g.active = map[uint]bool{}
	}
	g.block = false
}
