package config

type YARPConfig struct {
	TCP *[]IPRule `mapstructure:"tcp"`
	UDP *[]IPRule `mapstructure:"udp"`

	Http  *Http `mapstructure:"http"`
	Https *Http `mapstructure:"https"`
}

type IPRule struct {
	BindAddr string `mapstructure:"bindAddr"`
	Target   string `mapstructure:"target"`
}

type Http struct {
	BindAddr string     `mapstructure:"bindAddr"`
	Rules    []HostRule `mapstructure:"rules"`
}

type HostRule struct {
	Host   string `mapstructure:"host"`
	Target string `mapstructure:"target"`
}
