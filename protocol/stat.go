package protocol

import (
	"sync"
	"sync/atomic"
	"time"
)

func init() {
	GlobalStats.Start()
}

type RuleStats struct {
	BytesIn     uint64
	BytesOut    uint64
	ConnCount   int32
	RateInKBps  float64
	RateOutKBps float64
}

type StatsManager struct {
	mu    sync.RWMutex
	stats map[string]*RuleStats
}

type Snapshot struct {
	RuleStats      map[string]RuleStats `json:"ruleStats"`
	LastUpdateTime time.Time            `json:"lastUpdateTime"`
}

var GlobalStats = &StatsManager{
	stats: make(map[string]*RuleStats),
}

func (m *StatsManager) getOrCreateRule(key string) *RuleStats {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.stats[key]; ok {
		return s
	}
	s := &RuleStats{}
	m.stats[key] = s
	return s
}

func (m *StatsManager) AddConn(key string) {
	s := m.getOrCreateRule(key)
	atomic.AddInt32(&s.ConnCount, 1)
}

func (m *StatsManager) RemoveConn(key string) {
	s := m.getOrCreateRule(key)
	atomic.AddInt32(&s.ConnCount, -1)
}

func (m *StatsManager) AddBytes(key string, in, out int64) {
	s := m.getOrCreateRule(key)
	atomic.AddUint64(&s.BytesIn, uint64(in))
	atomic.AddUint64(&s.BytesOut, uint64(out))
}

func (m *StatsManager) Snapshot() Snapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	snapshot := Snapshot{
		RuleStats:      make(map[string]RuleStats, len(m.stats)),
		LastUpdateTime: time.Now(),
	}

	for k, v := range m.stats {
		snapshot.RuleStats[k] = RuleStats{
			BytesIn:     atomic.LoadUint64(&v.BytesIn),
			BytesOut:    atomic.LoadUint64(&v.BytesOut),
			ConnCount:   atomic.LoadInt32(&v.ConnCount),
			RateInKBps:  v.RateInKBps,
			RateOutKBps: v.RateOutKBps,
		}
	}
	return snapshot
}

func (m *StatsManager) Start() {
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		prev := Snapshot{
			RuleStats: make(map[string]RuleStats),
		}
		for range ticker.C {
			now := m.Snapshot()
			m.mu.Lock()
			for key, curr := range now.RuleStats {
				last := prev.RuleStats[key]
				inDelta := float64(curr.BytesIn - last.BytesIn)
				outDelta := float64(curr.BytesOut - last.BytesOut)
				s := m.stats[key]
				s.RateInKBps = inDelta / 1024.0
				s.RateOutKBps = outDelta / 1024.0
			}
			m.mu.Unlock()
			prev = now
		}
	}()
}
