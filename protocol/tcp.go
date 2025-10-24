package protocol

import (
	"fmt"
	"net"

	"k8s.io/klog/v2"

	"github.com/knwgo/yarp/config"
)

type TcpProxy struct {
	cfg []config.IPRule
}

func NewTcpProxy(cfg []config.IPRule) *TcpProxy {
	return &TcpProxy{
		cfg: cfg,
	}
}

func (t TcpProxy) Start() error {
	for _, rule := range t.cfg {
		ln, err := net.Listen("tcp", rule.BindAddr)
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
				klog.Infof("[tcp] new conn form %s, %s -> %s", conn.RemoteAddr(), bindAddr, target)

				go t.handleConnection(conn, target, bindAddr)
			}
		}(ln, rule.Target, rule.BindAddr)
	}

	return nil
}

func (t TcpProxy) handleConnection(conn net.Conn, target, bindAddr string) {
	ruleKey := fmt.Sprintf("tcp:%s->%s", bindAddr, target)

	targetConn, err := net.Dial("tcp", target)
	if err != nil {
		klog.Errorf("failed to dial target: %v", err)
		return
	}

	if err := pipeWithStats(conn, targetConn, ruleKey); err != nil {
		klog.Errorf("failed to pipe connection: %v", err)
	}
}
