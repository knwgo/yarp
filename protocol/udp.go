package protocol

import (
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"k8s.io/klog/v2"

	"github.com/knwgo/yarp/config"
)

type UdpProxy struct {
	cfg []config.IPRule
}

func NewUdpProxy(cfg []config.IPRule) *UdpProxy {
	return &UdpProxy{cfg: cfg}
}

func (u *UdpProxy) Start() error {
	for _, rule := range u.cfg {
		pc, err := net.ListenPacket("udp", rule.BindAddr)
		if err != nil {
			return err
		}
		go u.handleListener(pc, rule.Target, rule.BindAddr)
	}
	return nil
}

type countingBuffer struct {
	ruleKey string
	inBuf   int64
	outBuf  int64
	limit   int64
	mu      sync.Mutex
}

func newCountingBuffer(ruleKey string, limit int64) *countingBuffer {
	return &countingBuffer{
		ruleKey: ruleKey,
		limit:   limit,
	}
}

func (cb *countingBuffer) add(in, out int64) {
	cb.mu.Lock()
	cb.inBuf += in
	cb.outBuf += out
	if cb.inBuf >= cb.limit || cb.outBuf >= cb.limit {
		GlobalStats.AddBytes(cb.ruleKey, cb.inBuf, cb.outBuf)
		cb.inBuf = 0
		cb.outBuf = 0
	}
	cb.mu.Unlock()
}

type activePeers struct {
	peers map[string]time.Time
	mu    sync.Mutex
}

func newActivePeers() *activePeers {
	return &activePeers{
		peers: make(map[string]time.Time),
	}
}

func (ap *activePeers) add(addr string) {
	ap.mu.Lock()
	ap.peers[addr] = time.Now()
	ap.mu.Unlock()
}

func (ap *activePeers) count(expire time.Duration) int32 {
	ap.mu.Lock()
	now := time.Now()
	for k, t := range ap.peers {
		if now.Sub(t) > expire {
			delete(ap.peers, k)
		}
	}
	cnt := int32(len(ap.peers))
	ap.mu.Unlock()
	return cnt
}

func (u *UdpProxy) handleListener(pc net.PacketConn, targetAddr, bindAddr string) {
	buf := make([]byte, 64*1024)
	ruleKey := fmt.Sprintf("udp:%s->%s", bindAddr, targetAddr)
	cb := newCountingBuffer(ruleKey, 8*1024)
	active := newActivePeers()

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	go func() {
		for range ticker.C {
			cnt := active.count(60 * time.Second)
			GlobalStats.mu.Lock()
			s := GlobalStats.getOrCreateRule(ruleKey)
			atomic.StoreInt32(&s.ConnCount, cnt)
			GlobalStats.mu.Unlock()
		}
	}()

	for {
		n, addr, err := pc.ReadFrom(buf)
		if err != nil {
			klog.Errorf("udp read error: %v", err)
			continue
		}
		active.add(addr.String())

		go func(data []byte, n int, srcAddr net.Addr) {
			targetConn, err := net.Dial("udp", targetAddr)
			if err != nil {
				klog.Errorf("udp dial target error: %v", err)
				return
			}
			defer func() {
				_ = targetConn.Close()
			}()

			written, err := targetConn.Write(data[:n])
			if err != nil {
				klog.Errorf("udp write target error: %v", err)
				return
			}

			cb.add(int64(written), int64(n))

			respBuf := make([]byte, 64*1024)
			_ = targetConn.SetReadDeadline(time.Now().Add(3 * time.Second))
			rl, err := targetConn.Read(respBuf)
			if err == nil && rl > 0 {
				cb.add(int64(rl), 0)
				_, _ = pc.WriteTo(respBuf[:rl], srcAddr)
			}
		}(append([]byte(nil), buf[:n]...), n, addr)
	}
}
