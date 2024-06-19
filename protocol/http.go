package protocol

import (
	"net/http"
	"net/http/httputil"

	"k8s.io/klog/v2"

	"github.com/kaynAw/yarp/config"
)

type HTTPProxy struct {
	Cfg config.Http
}

func (hp HTTPProxy) handleReverse(w http.ResponseWriter, r *http.Request) {
	targetUrl, err := getTargetUrl(r.Host, hp.Cfg.Rules)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	targetUrl.Scheme = "http"
	klog.Infof("[http] new conn from %s, %s -> %s", r.RemoteAddr, r.Host, targetUrl)
	httputil.NewSingleHostReverseProxy(targetUrl).ServeHTTP(w, r)
}

func (hp HTTPProxy) Start() error {
	http.HandleFunc("/", hp.handleReverse)
	return http.ListenAndServe(hp.Cfg.BindAddr, nil)
}
