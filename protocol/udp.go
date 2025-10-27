package protocol

import (
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"k8s.io/klog/v2"

	"github.com/knwgo/yarp/config"
	"github.com/knwgo/yarp/stat"
)

type UdpProxy struct {
	cfg []config.IPRule
}

func NewUdpProxy(cfg []config.IPRule) *UdpProxy {
	return &UdpProxy{cfg: cfg}
}

type session struct {
	clientAddr *net.UDPAddr
	targetConn *net.UDPConn

	writeCh chan []byte
	closed  chan struct{}
	closeMu sync.Mutex
	closedF bool

	lastActive atomic.Int64 // unix nano

	pendingLock sync.Mutex
	pendingIn   int64 // dest -> src
	pendingOut  int64 // src -> dest
}

func newSession(clientAddr *net.UDPAddr, targetConn *net.UDPConn) *session {
	s := &session{
		clientAddr: clientAddr,
		targetConn: targetConn,
		writeCh:    make(chan []byte, 256),
		closed:     make(chan struct{}),
	}
	s.touch()
	return s
}

func (s *session) touch() {
	s.lastActive.Store(time.Now().UnixNano())
}

func (s *session) isClosed() bool {
	s.closeMu.Lock()
	defer s.closeMu.Unlock()
	return s.closedF
}

func (s *session) close() {
	s.closeMu.Lock()
	defer s.closeMu.Unlock()
	if !s.closedF {
		s.closedF = true
		close(s.closed)
		_ = s.targetConn.Close()
	}
}

func (u *UdpProxy) Start() error {
	for _, rule := range u.cfg {
		pc, err := net.ListenPacket("udp", rule.BindAddr)
		if err != nil {
			return err
		}
		go startUDPListener(pc, rule.Target, rule.BindAddr)
	}
	return nil
}

func startUDPListener(pc net.PacketConn, targetAddr string, bindAddr string) {
	ruleKey := fmt.Sprintf("udp:%s->%s", bindAddr, targetAddr)

	// sessions map: clientAddr.String() -> *session
	sessions := make(map[string]*session)
	var sessionsMu sync.Mutex

	const (
		idleTimeout           = 90 * time.Second
		cleanupInterval       = 30 * time.Second
		flushInterval         = 1 * time.Second
		pendingFlushThreshold = 16 * 1024
	)

	rs := stat.GlobalStats.GetOrCreateRule(ruleKey)

	go func() {
		ticker := time.NewTicker(flushInterval)
		defer ticker.Stop()
		for range ticker.C {
			var totalIn int64
			var totalOut int64
			var activeCount int32

			sessionsMu.Lock()
			now := time.Now()
			for k, s := range sessions {
				last := time.Unix(0, s.lastActive.Load())
				if now.Sub(last) <= idleTimeout {
					activeCount++
				}

				s.pendingLock.Lock()
				in := s.pendingIn
				out := s.pendingOut
				s.pendingIn = 0
				s.pendingOut = 0
				s.pendingLock.Unlock()

				totalIn += in
				totalOut += out

				if s.isClosed() {
					delete(sessions, k)
				}
			}
			sessionsMu.Unlock()

			if totalIn != 0 || totalOut != 0 {
				stat.GlobalStats.AddBytes(ruleKey, totalIn, totalOut)
			}

			atomic.StoreInt32(&rs.ConnCount, activeCount)
		}
	}()

	go func() {
		ticker := time.NewTicker(cleanupInterval)
		defer ticker.Stop()
		for range ticker.C {
			now := time.Now()
			sessionsMu.Lock()
			for key, s := range sessions {
				last := time.Unix(0, s.lastActive.Load())
				if now.Sub(last) > idleTimeout {
					s.pendingLock.Lock()
					in := s.pendingIn
					out := s.pendingOut
					s.pendingIn = 0
					s.pendingOut = 0
					s.pendingLock.Unlock()

					if in != 0 || out != 0 {
						stat.GlobalStats.AddBytes(ruleKey, in, out)
					}

					s.close()
					delete(sessions, key)
				}
			}
			sessionsMu.Unlock()
		}
	}()

	buf := make([]byte, 64*1024)
	for {
		n, addr, err := pc.ReadFrom(buf)
		if err != nil {
			klog.Errorf("[udp] read error on %s: %v", bindAddr, err)
			continue
		}
		udpAddr, ok := addr.(*net.UDPAddr)
		if !ok {
			continue
		}

		pkt := append([]byte(nil), buf[:n]...)
		key := udpAddr.String()

		sessionsMu.Lock()
		sess, ok := sessions[key]
		if !ok {
			// Dial UDP target once for this client session
			targetUDPAddr, err := net.ResolveUDPAddr("udp", targetAddr)
			if err != nil {
				klog.Errorf("[udp] resolve target %s error: %v", targetAddr, err)
				sessionsMu.Unlock()
				continue
			}
			tc, err := net.DialUDP("udp", nil, targetUDPAddr)
			if err != nil {
				klog.Errorf("[udp] dial target %s error: %v", targetAddr, err)
				sessionsMu.Unlock()
				continue
			}

			sess = newSession(udpAddr, tc)
			sessions[key] = sess

			go func(s *session) {
				readBuf := make([]byte, 64*1024)
				for {
					select {
					case <-s.closed:
						return
					default:
					}

					nr, err := s.targetConn.Read(readBuf)
					if err != nil {
						return
					}
					if nr <= 0 {
						continue
					}

					s.pendingLock.Lock()
					s.pendingIn += int64(nr)
					s.pendingLock.Unlock()

					_, _ = pc.WriteTo(readBuf[:nr], s.clientAddr)
					s.touch()
				}
			}(sess)

			go func(s *session) {
				for {
					select {
					case data := <-s.writeCh:
						if data == nil {
							return
						}
						w, err := s.targetConn.Write(data)
						if err != nil {
							return
						}
						// 增加 pendingOut，并在超过阈值时尽快写回 GlobalStats（避免长时间占用内存）
						s.pendingLock.Lock()
						s.pendingOut += int64(w)
						inP := s.pendingIn
						outP := s.pendingOut
						if inP >= pendingFlushThreshold || outP >= pendingFlushThreshold {
							s.pendingIn = 0
							s.pendingOut = 0
							s.pendingLock.Unlock()
							stat.GlobalStats.AddBytes(ruleKey, inP, outP)
						} else {
							s.pendingLock.Unlock()
						}
						s.touch()
					case <-s.closed:
						return
					}
				}
			}(sess)
		}

		sess.touch()

		select {
		case sess.writeCh <- pkt:
		default:
			sess.pendingLock.Lock()
			sess.pendingOut += int64(len(pkt))
			sess.pendingLock.Unlock()
		}
		sessionsMu.Unlock()
	}
}
