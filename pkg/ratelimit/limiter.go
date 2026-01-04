package ratelimit

import (
	"context"
	"sync"
	"time"
)

// Limiter 令牌桶限流器
type Limiter struct {
	rate       float64     // 每秒产生的令牌数
	burst      int         // 桶容量
	tokens     float64     // 当前令牌数
	lastUpdate time.Time   // 上次更新时间
	mu         sync.Mutex
}

// NewLimiter 创建限流器
// rate: 每秒允许的请求数
// burst: 突发容量
func NewLimiter(rate float64, burst int) *Limiter {
	return &Limiter{
		rate:       rate,
		burst:      burst,
		tokens:     float64(burst),
		lastUpdate: time.Now(),
	}
}

// Allow 检查是否允许请求
func (l *Limiter) Allow() bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(l.lastUpdate).Seconds()
	l.lastUpdate = now

	// 补充令牌
	l.tokens += elapsed * l.rate
	if l.tokens > float64(l.burst) {
		l.tokens = float64(l.burst)
	}

	// 检查是否有令牌
	if l.tokens >= 1 {
		l.tokens--
		return true
	}

	return false
}

// Wait 等待直到获取令牌
func (l *Limiter) Wait(ctx context.Context) error {
	for {
		if l.Allow() {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Millisecond * 100):
			// 继续尝试
		}
	}
}

// RateLimitedClient 带限流的客户端包装
type RateLimitedClient struct {
	limiter *Limiter
}

// NewRateLimitedClient 创建带限流的客户端
func NewRateLimitedClient(requestsPerSecond float64, burst int) *RateLimitedClient {
	return &RateLimitedClient{
		limiter: NewLimiter(requestsPerSecond, burst),
	}
}

// Allow 检查是否允许请求
func (c *RateLimitedClient) Allow() bool {
	return c.limiter.Allow()
}

// Wait 等待直到获取令牌
func (c *RateLimitedClient) Wait(ctx context.Context) error {
	return c.limiter.Wait(ctx)
}

// MultiLimiter 多限流器管理
type MultiLimiter struct {
	limiters map[string]*Limiter
	mu       sync.RWMutex
}

// NewMultiLimiter 创建多限流器
func NewMultiLimiter() *MultiLimiter {
	return &MultiLimiter{
		limiters: make(map[string]*Limiter),
	}
}

// GetLimiter 获取或创建限流器
func (m *MultiLimiter) GetLimiter(key string, rate float64, burst int) *Limiter {
	m.mu.RLock()
	if limiter, ok := m.limiters[key]; ok {
		m.mu.RUnlock()
		return limiter
	}
	m.mu.RUnlock()

	m.mu.Lock()
	defer m.mu.Unlock()

	// 双重检查
	if limiter, ok := m.limiters[key]; ok {
		return limiter
	}

	limiter := NewLimiter(rate, burst)
	m.limiters[key] = limiter
	return limiter
}

// Allow 检查指定 key 是否允许请求
func (m *MultiLimiter) Allow(key string, rate float64, burst int) bool {
	return m.GetLimiter(key, rate, burst).Allow()
}

// 预定义的限流配置
var (
	// LarkAPILimiter 飞书 API 限流（每秒 10 次，突发 20）
	LarkAPILimiter = NewLimiter(10, 20)

	// LLMAPILimiter LLM API 限流（每秒 5 次，突发 10）
	LLMAPILimiter = NewLimiter(5, 10)

	// GitHubAPILimiter GitHub API 限流（每秒 5 次，突发 10）
	GitHubAPILimiter = NewLimiter(5, 10)

	// UserLimiters 用户级别限流（每用户每秒 2 次）
	UserLimiters = NewMultiLimiter()
)

// AllowLarkAPI 检查飞书 API 是否允许请求
func AllowLarkAPI() bool {
	return LarkAPILimiter.Allow()
}

// AllowLLMAPI 检查 LLM API 是否允许请求
func AllowLLMAPI() bool {
	return LLMAPILimiter.Allow()
}

// AllowGitHubAPI 检查 GitHub API 是否允许请求
func AllowGitHubAPI() bool {
	return GitHubAPILimiter.Allow()
}

// AllowUser 检查用户是否允许请求（防止单用户刷请求）
func AllowUser(userID string) bool {
	return UserLimiters.Allow(userID, 2, 5)
}
