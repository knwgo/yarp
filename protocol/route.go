package protocol

import (
	"errors"
	"net"
	"net/url"
	"strings"

	"k8s.io/klog/v2"

	"github.com/knwgo/yarp/config"
)

type targetInfo struct {
	url      *url.URL
	wsEnabled bool
}

func getTargetUrl(srcHostPort string, rules []config.HostRule) (*targetInfo, error) {
	var host string
	var err error

	host, _, err = net.SplitHostPort(srcHostPort)
	if err != nil {
		host = srcHostPort
	}

	var target string
	var wsEnabled bool

	for _, rule := range rules {
		if len(rule.Host) == 0 || len(rule.Target) == 0 {
			klog.Fatal("host or target host are empty")
		}

		if rule.Host[0] == '*' {
			if len(rule.Host) < 2 {
				klog.Fatalf("invalid host format: %v", rule.Host)
			}

			if strings.HasSuffix(host, rule.Host[1:]) {
				target = rule.Target
				wsEnabled = rule.Ws != nil && *rule.Ws
				break
			}
		} else {
			if host == rule.Host {
				target = rule.Target
				wsEnabled = rule.Ws != nil && *rule.Ws
				break
			}
		}
	}

	if len(target) == 0 {
		return nil, errors.New("no host found")
	}

	return &targetInfo{url: &url.URL{Host: target}, wsEnabled: wsEnabled}, nil
}
