// Package httpx 提供刮削数据源专用的 HTTP 客户端。
//
// 每个数据源实例持有自己的 Client：独立连接池、按 host 的令牌桶限速、
// 有界并发、整体请求超时，以及只对网络错误/429/5xx 生效的重试。
// 它替代 go-common tools.ReqWithProxy —— 后者对成功的 GET 也会重试，
// 导致每个请求实际发出 4 次。
package httpx

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"izumi/config"

	zaplog "github.com/dokidokikoi/go-common/log/zap"
	"go.uber.org/zap"
	"golang.org/x/sync/semaphore"
	"golang.org/x/time/rate"
	"resty.dev/v3"
)

// HostLimit 是单个 host 的限速覆盖。零值字段回落到 Clients 的默认值。
type HostLimit struct {
	QPS   float64
	Burst int
}

// Settings 是 Client 的可调参数。QPS<0 表示不限速，MaxConcurrency<0 表示不限并发。
type Settings struct {
	Timeout        time.Duration
	MaxRetries     int
	RetryWait      time.Duration
	RetryMaxWait   time.Duration
	DefaultQPS     float64
	DefaultBurst   int
	MaxConcurrency int
	HostLimits     map[string]HostLimit
}

// Option 允许调用方（主要是测试）覆盖默认配置。
type Option func(*Settings)

// WithSettings 整体替换从 config 读取的设置。
func WithSettings(s Settings) Option {
	return func(dst *Settings) { *dst = s }
}

// Client 是一个数据源专用的 HTTP 客户端。它可并发使用。
type Client struct {
	site     string
	rc       *resty.Client
	settings Settings
	hosts    sync.Map // host -> *rate.Limiter
	sem      *semaphore.Weighted
	proxy    atomic.Pointer[url.URL]
}

// New 创建指定数据源的客户端。proxy 为空时使用环境变量代理。
func New(site, proxy string, opts ...Option) *Client {
	s := settingsFromConfig(site)
	for _, opt := range opts {
		opt(&s)
	}
	s = normalizeSettings(s)

	c := &Client{site: site, settings: s}
	c.rc = resty.NewWithTransportSettings(&resty.TransportSettings{
		DialerTimeout:       30 * time.Second,
		DialerKeepAlive:     30 * time.Second,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 16,
	})
	c.rc.SetRetryCount(s.MaxRetries).
		SetRetryWaitTime(s.RetryWait).
		SetRetryMaxWaitTime(s.RetryMaxWait).
		// resty 默认条件：仅网络错误/429/5xx/0；不要添加恒真条件。
		SetRetryDefaultConditions(true).
		// vndb/bangumi/ggbases 的 POST 是只读查询，允许瞬时错误重试。
		SetAllowNonIdempotentRetry(true).
		// 让 Do 返回后调用方仍可读 body（ctx cancel 后也能读）。
		SetResponseBodyUnlimitedReads(true)

	if tr, err := c.rc.HTTPTransport(); err == nil {
		tr.Proxy = c.proxyFor
	}
	// 限速中间件必须在 PrepareRequestMiddleware 之后，才能拿到解析好的 req.URL。
	c.rc.SetRequestMiddlewares(resty.PrepareRequestMiddleware, c.rateLimitMiddleware)

	if s.MaxConcurrency > 0 {
		c.sem = semaphore.NewWeighted(int64(s.MaxConcurrency))
	}
	c.SetProxy(proxy)
	return c
}

func settingsFromConfig(site string) Settings {
	cfg := config.ScraperHTTPFor(site)
	s := Settings{
		Timeout:        time.Duration(cfg.TimeoutSeconds) * time.Second,
		MaxRetries:     cfg.MaxRetries,
		RetryWait:      time.Duration(cfg.RetryWaitMillis) * time.Millisecond,
		RetryMaxWait:   time.Duration(cfg.RetryMaxWaitMillis) * time.Millisecond,
		DefaultQPS:     cfg.QPS,
		DefaultBurst:   cfg.Burst,
		MaxConcurrency: cfg.MaxConcurrency,
		HostLimits:     make(map[string]HostLimit, len(cfg.Hosts)),
	}
	for _, h := range cfg.Hosts {
		s.HostLimits[h.Host] = HostLimit{QPS: h.QPS, Burst: h.Burst}
	}
	return s
}

func normalizeSettings(s Settings) Settings {
	if s.Timeout <= 0 {
		s.Timeout = 30 * time.Second
	}
	if s.MaxRetries < 0 {
		s.MaxRetries = 0
	}
	if s.RetryWait <= 0 {
		s.RetryWait = 250 * time.Millisecond
	}
	if s.RetryMaxWait < s.RetryWait {
		s.RetryMaxWait = 1500 * time.Millisecond
	}
	if s.DefaultBurst <= 0 && s.DefaultQPS > 0 {
		s.DefaultBurst = int(s.DefaultQPS) + 1
	}
	return s
}

// SetProxy 更新代理。空串表示回落到环境变量代理（http_proxy/https_proxy）。
func (c *Client) SetProxy(proxy string) {
	proxy = strings.TrimSpace(proxy)
	if proxy == "" {
		c.proxy.Store(nil)
		return
	}
	u, err := url.Parse(proxy)
	if err != nil || u.Host == "" {
		zaplog.L().With(zap.String("site", c.site), zap.String("proxy", proxy), zap.Error(err)).
			Error("invalid scraper proxy, fallback to environment proxy")
		c.proxy.Store(nil)
		return
	}
	c.proxy.Store(u)
}

// proxyFor 是 Transport.Proxy 的实现：显式代理优先，否则走环境变量。
// 通过 atomic 读取，运行期 SetProxy 与在途请求无数据竞争。
func (c *Client) proxyFor(req *http.Request) (*url.URL, error) {
	if u := c.proxy.Load(); u != nil {
		return u, nil
	}
	return http.ProxyFromEnvironment(req)
}

// MaxConcurrency 返回数据源级在途请求上限（<=0 表示不限）。
func (c *Client) MaxConcurrency() int {
	return c.settings.MaxConcurrency
}

func (c *Client) rateLimitMiddleware(_ *resty.Client, r *resty.Request) error {
	// 该中间件排在 PrepareRequestMiddleware 之后，此时 RawRequest 已构建。
	if r.RawRequest == nil || r.RawRequest.URL == nil {
		return nil
	}
	limiter := c.limiterFor(r.RawRequest.URL.Hostname())
	if limiter == nil {
		return nil
	}
	return limiter.Wait(r.Context())
}

func (c *Client) limiterFor(host string) *rate.Limiter {
	qps, burst := c.limitFor(host)
	if qps <= 0 {
		return nil
	}
	if v, ok := c.hosts.Load(host); ok {
		return v.(*rate.Limiter)
	}
	limiter := rate.NewLimiter(rate.Limit(qps), burst)
	actual, loaded := c.hosts.LoadOrStore(host, limiter)
	if loaded {
		return actual.(*rate.Limiter)
	}
	return limiter
}

func (c *Client) limitFor(host string) (qps float64, burst int) {
	qps, burst = c.settings.DefaultQPS, c.settings.DefaultBurst
	if h, ok := c.settings.HostLimits[host]; ok {
		if h.QPS != 0 {
			qps = h.QPS
		}
		if h.Burst > 0 {
			burst = h.Burst
		}
	}
	if burst <= 0 && qps > 0 {
		burst = int(qps) + 1
	}
	return qps, burst
}

// Do 执行一次请求（含重试、限速、并发上限与整体超时）。
// 返回值语义与 resty 一致：非 2xx 不返回 error，由调用方按 status code 判断。
func (c *Client) Do(method, uri string, headers map[string]string, body any) (*resty.Response, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.settings.Timeout)
	defer cancel()

	if c.sem != nil {
		if err := c.sem.Acquire(ctx, 1); err != nil {
			return nil, err
		}
		defer c.sem.Release(1)
	}

	req := c.rc.R().SetContext(ctx)
	if len(headers) > 0 {
		req.SetHeaders(headers)
	}
	if body != nil {
		req.SetBody(body)
	}
	return req.Execute(method, uri)
}
