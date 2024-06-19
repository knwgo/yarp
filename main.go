package main

import (
	"flag"

	"github.com/spf13/viper"
	"golang.org/x/sync/errgroup"
	"k8s.io/klog/v2"

	"github.com/kaynAw/yarp/config"
	"github.com/kaynAw/yarp/protocol"
)

func main() {
	var cfgFile string
	flag.StringVar(&cfgFile, "c", "./yarp.toml", "config file")
	klog.InitFlags(nil)
	_ = flag.Set("log_dir", "./")
	_ = flag.Set("logtostderr", "false")
	flag.Parse()

	defer klog.Flush()

	viper.SetConfigFile(cfgFile)
	if err := viper.ReadInConfig(); err != nil {
		klog.Fatalf("couldn't load config: %s", err)
	}

	var YARPConfig config.YARPConfig
	if err := viper.Unmarshal(&YARPConfig); err != nil {
		klog.Fatal(err)
	}

	eg := errgroup.Group{}

	if YARPConfig.TCP != nil {
		klog.Info("starting tcp proxy")
		eg.Go(protocol.NewTCPProxy(*YARPConfig.TCP, "tcp").Start)
	}

	if YARPConfig.UDP != nil {
		klog.Info("starting udp proxy")
		eg.Go(protocol.NewTCPProxy(*YARPConfig.UDP, "udp").Start)
	}

	if YARPConfig.Http != nil {
		klog.Info("starting http proxy")
		eg.Go(protocol.HTTPProxy{Cfg: *YARPConfig.Http}.Start)
	}

	if YARPConfig.Https != nil {
		klog.Info("starting https proxy")
		eg.Go(protocol.HTTPSProxy{Cfg: *YARPConfig.Https}.Start)
	}

	klog.Error(eg.Wait())
}
