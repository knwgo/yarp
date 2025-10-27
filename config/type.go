package config

type YARPConfig struct {
	TCP *[]IPRule `mapstructure:"tcp"`
	UDP *[]IPRule `mapstructure:"udp"`

	Http  *Http `mapstructure:"http"`
	Https *Http `mapstructure:"https"`

	Dashboard *Dashboard `mapstructure:"dashboard"`
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

type Dashboard struct {
	BindAddr     string `mapstructure:"bindAddr"`
	HttpUser     string `mapstructure:"httpUser"`
	HttpPassword string `mapstructure:"httpPassword"`
}
