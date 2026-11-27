package config

// ScraperConfig 是刮削数据源详情抓取的网络与并发配置。
// 可通过 application.yaml 的 scraper 段配置，运行期可用 IZUMI_SCRAPER_* 环境变量覆盖。
type ScraperConfig struct {
	HTTP  ScraperHTTPConfig  `mapstructure:"http"`
	Image ScraperImageConfig `mapstructure:"image"`
}

// ScraperHTTPConfig 描述 HTTP 公共层的默认参数。
//
// 约定：QPS/Burst/MaxConcurrency 为 0（或未配置）表示使用默认值，
// 为负数表示显式关闭限制（QPS<0 不限速，MaxConcurrency<0 不限并发）。
type ScraperHTTPConfig struct {
	TimeoutSeconds     int                          `mapstructure:"timeout_seconds"`
	MaxRetries         int                          `mapstructure:"max_retries"`
	RetryWaitMillis    int                          `mapstructure:"retry_wait_ms"`
	RetryMaxWaitMillis int                          `mapstructure:"retry_max_wait_ms"`
	QPS                float64                      `mapstructure:"qps"`
	Burst              int                          `mapstructure:"burst"`
	MaxConcurrency     int                          `mapstructure:"max_concurrency"`
	Sites              map[string]ScraperSiteConfig `mapstructure:"sites"`
	Hosts              []ScraperHostConfig          `mapstructure:"hosts"`
}

// ScraperSiteConfig 是单个数据源的覆盖配置；零值表示继承全局值。
type ScraperSiteConfig struct {
	QPS            float64 `mapstructure:"qps"`
	Burst          int     `mapstructure:"burst"`
	MaxConcurrency int     `mapstructure:"max_concurrency"`
	TimeoutSeconds int     `mapstructure:"timeout_seconds"`
	MaxRetries     int     `mapstructure:"max_retries"`
}

// ScraperHostConfig 是单个 host 的限速覆盖；零值表示继承站点级配置。
type ScraperHostConfig struct {
	Host  string  `mapstructure:"host"`
	QPS   float64 `mapstructure:"qps"`
	Burst int     `mapstructure:"burst"`
}

// ScraperImageConfig 描述刮削期间图片批量下载的参数。
type ScraperImageConfig struct {
	SaveConcurrency int `mapstructure:"save_concurrency"`
}

const (
	defaultScraperTimeoutSeconds     = 30
	defaultScraperMaxRetries         = 2
	defaultScraperRetryWaitMillis    = 250
	defaultScraperRetryMaxWaitMillis = 1500
	defaultScraperQPS                = 3
	defaultScraperBurst              = 6
	defaultScraperMaxConcurrency     = 6
	defaultImageSaveConcurrency      = 8
)

// DefaultScraperConfig 返回代码内置默认值。它同时作为 yaml 缺失字段的回填来源，
// 因此必须保持自洽（QPS/Burst 为正、并发数固定）。
func DefaultScraperConfig() ScraperConfig {
	return ScraperConfig{
		HTTP: ScraperHTTPConfig{
			TimeoutSeconds:     defaultScraperTimeoutSeconds,
			MaxRetries:         defaultScraperMaxRetries,
			RetryWaitMillis:    defaultScraperRetryWaitMillis,
			RetryMaxWaitMillis: defaultScraperRetryMaxWaitMillis,
			QPS:                defaultScraperQPS,
			Burst:              defaultScraperBurst,
			MaxConcurrency:     defaultScraperMaxConcurrency,
			Hosts: []ScraperHostConfig{
				{Host: "api.vndb.org", QPS: 0.6, Burst: 3},
				{Host: "t.vndb.org", QPS: 8, Burst: 16},
				{Host: "api.bgm.tv", QPS: 3, Burst: 6},
				{Host: "lain.bgm.tv", QPS: 8, Burst: 16},
				{Host: "www.dlsite.com", QPS: 3, Burst: 6},
				{Host: "img.dlsite.jp", QPS: 8, Burst: 16},
				{Host: "www.getchu.com", QPS: 3, Burst: 6},
				{Host: "2dfan.com", QPS: 3, Burst: 6},
				{Host: "img.achost.top", QPS: 8, Burst: 16},
			},
		},
		Image: ScraperImageConfig{SaveConcurrency: defaultImageSaveConcurrency},
	}
}

// GetScraperConfig 返回合并后的配置。configIns 为 nil（例如单测直接构造 scraper）时
// 返回内置默认值，避免 nil 解引用与 rate.NewLimiter(0,0) 永久阻塞。
func GetScraperConfig() ScraperConfig {
	def := DefaultScraperConfig()
	if configIns == nil {
		return def
	}

	c := configIns.ScraperConfig
	if c.HTTP.TimeoutSeconds <= 0 {
		c.HTTP.TimeoutSeconds = def.HTTP.TimeoutSeconds
	}
	if c.HTTP.RetryWaitMillis <= 0 {
		c.HTTP.RetryWaitMillis = def.HTTP.RetryWaitMillis
	}
	if c.HTTP.RetryMaxWaitMillis <= 0 {
		c.HTTP.RetryMaxWaitMillis = def.HTTP.RetryMaxWaitMillis
	}
	if c.HTTP.QPS == 0 {
		c.HTTP.QPS = def.HTTP.QPS
	}
	if c.HTTP.Burst <= 0 {
		c.HTTP.Burst = def.HTTP.Burst
	}
	if c.HTTP.MaxConcurrency == 0 {
		c.HTTP.MaxConcurrency = def.HTTP.MaxConcurrency
	}
	if c.HTTP.Sites == nil {
		c.HTTP.Sites = def.HTTP.Sites
	}
	// hosts 以默认值为底，yaml 中同 host 的条目覆盖默认值。
	hostM := make(map[string]ScraperHostConfig, len(def.HTTP.Hosts)+len(c.HTTP.Hosts))
	order := make([]string, 0, len(def.HTTP.Hosts)+len(c.HTTP.Hosts))
	for _, h := range def.HTTP.Hosts {
		if _, ok := hostM[h.Host]; !ok {
			order = append(order, h.Host)
		}
		hostM[h.Host] = h
	}
	for _, h := range c.HTTP.Hosts {
		if h.Host == "" {
			continue
		}
		if _, ok := hostM[h.Host]; !ok {
			order = append(order, h.Host)
		}
		hostM[h.Host] = h
	}
	c.HTTP.Hosts = make([]ScraperHostConfig, 0, len(order))
	for _, host := range order {
		c.HTTP.Hosts = append(c.HTTP.Hosts, hostM[host])
	}

	if c.Image.SaveConcurrency <= 0 {
		c.Image.SaveConcurrency = def.Image.SaveConcurrency
	}
	return c
}

// ScraperHTTPFor 返回指定数据源合并后的 HTTP 参数：全局值 -> sites[name] 覆盖。
func ScraperHTTPFor(name string) ScraperHTTPConfig {
	cfg := GetScraperConfig().HTTP
	site, ok := cfg.Sites[name]
	if !ok {
		return cfg
	}
	if site.QPS != 0 {
		cfg.QPS = site.QPS
	}
	if site.Burst > 0 {
		cfg.Burst = site.Burst
	}
	if site.MaxConcurrency != 0 {
		cfg.MaxConcurrency = site.MaxConcurrency
	}
	if site.TimeoutSeconds > 0 {
		cfg.TimeoutSeconds = site.TimeoutSeconds
	}
	if site.MaxRetries > 0 {
		cfg.MaxRetries = site.MaxRetries
	}
	return cfg
}

// ScraperHostLimit 返回指定数据源在指定 host 上的限速。
// hosts 精确匹配优先；未命中时回落到站点级 QPS/Burst。
func ScraperHostLimit(name, host string) (qps float64, burst int) {
	cfg := ScraperHTTPFor(name)
	qps, burst = cfg.QPS, cfg.Burst
	for _, h := range cfg.Hosts {
		if h.Host != host {
			continue
		}
		if h.QPS != 0 {
			qps = h.QPS
		}
		if h.Burst > 0 {
			burst = h.Burst
		}
		break
	}
	if burst <= 0 && qps > 0 {
		burst = int(qps) + 1
		if burst < 1 {
			burst = 1
		}
	}
	return qps, burst
}

// ImageSaveConcurrency 返回 SaveBunchTmpFiles 的默认并发数。
func ImageSaveConcurrency() int {
	if configIns == nil {
		return defaultImageSaveConcurrency
	}
	n := configIns.ScraperConfig.Image.SaveConcurrency
	if n <= 0 {
		return defaultImageSaveConcurrency
	}
	return n
}
