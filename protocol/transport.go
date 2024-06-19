package protocol

import (
	"net"

	"k8s.io/klog/v2"

	"github.com/kaynAw/yarp/config"
)

type TransportProxy struct {
	cfg     []config.IPRule
	network string
}

func NewTCPProxy(cfg []config.IPRule, network string) *TransportProxy {
	return &TransportProxy{
		cfg:     cfg,
		network: network,
	}
}

func (t TransportProxy) Start() error {
	for _, rule := range t.cfg {
		ln, err := net.Listen(t.network, rule.BindAddr)
		if err != nil {
			return err
		}

		go func(listener net.Listener, bindAddr, target string) {
			for {
				conn, err := listener.Accept()
				if err != nil {
					klog.Errorf("failed to accept connection: %v", err)
					continue
				}
				klog.Infof("[%s] new conn form %s, %s -> %s", t.network, conn.RemoteAddr(), bindAddr, target)

				go t.handleConnection(conn, target)
			}
		}(ln, rule.BindAddr, rule.Target)
	}

	return nil
}

func (t TransportProxy) handleConnection(conn net.Conn, target string) {
	targetConn, err := net.Dial(t.network, target)
	if err != nil {
		klog.Errorf("failed to dial target: %v", err)
	}

	if err := pipe(conn, targetConn); err != nil {
		klog.Errorf("failed to pipe connection: %v", err)
	}
}
