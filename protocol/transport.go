package protocol

import (
	"fmt"
	"net"

	"k8s.io/klog/v2"

	"github.com/knwgo/yarp/config"
)

type TransportProxy struct {
	cfg     []config.IPRule
	network string
}

func NewTransportProxy(cfg []config.IPRule, network string) *TransportProxy {
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

		go func(listener net.Listener, target, bindAddr string) {
			for {
				conn, err := listener.Accept()
				if err != nil {
					klog.Errorf("failed to accept connection: %v", err)
					continue
				}
				klog.Infof("[%s] new conn form %s, %s -> %s", t.network, conn.RemoteAddr(), bindAddr, target)

				go t.handleConnection(conn, target, bindAddr)
			}
		}(ln, rule.Target, rule.BindAddr)
	}

	return nil
}

func (t TransportProxy) handleConnection(conn net.Conn, target, bindAddr string) {
	ruleKey := fmt.Sprintf("%s:%s->%s", t.network, bindAddr, target)

	targetConn, err := net.Dial(t.network, target)
	if err != nil {
		klog.Errorf("failed to dial target: %v", err)
		return
	}

	if err := pipeWithStats(conn, targetConn, ruleKey); err != nil {
		klog.Errorf("failed to pipe connection: %v", err)
	}
}
