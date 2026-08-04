package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"cpa-usage-keeper/internal/protocol"
	"cpa-usage-keeper/internal/service"
	"github.com/gin-gonic/gin"
)

const (
	ingestContextKey = "keeper_ingest_identity"
	ingestRate       = 60.0 / 60.0
	ingestBurst      = 20.0

	// defaultIngestRateMaxEntries caps how many distinct authenticated
	// credential IDs the limiter tracks concurrently. Bound the map so admin
	// credential churn (issue/rotate/revoke) cannot grow it without limit.
	// Only successfully authenticated credential IDs are keyed, so this is not
	// remotely floodable.
	defaultIngestRateMaxEntries = 4096
	// defaultIngestRateIdleTTL evicts limiter entries that have not been seen
	// for this duration, matching the LoginAttemptLimiter housekeeping style.
	defaultIngestRateIdleTTL = 30 * time.Minute
)

type IngestIdentity struct {
	InstanceID   string
	InstanceName string
	CredentialID string
	Scopes       []string
}

type ingestRateEntry struct {
	tokens  float64
	updated time.Time
}

type ingestRateLimiterOptions struct {
	rate       float64
	burst      float64
	maxEntries int
	idleTTL    time.Duration
	now        func() time.Time
}

type ingestRateLimiter struct {
	mu      sync.Mutex
	entries map[string]ingestRateEntry

	rate       float64
	burst      float64
	maxEntries int
	idleTTL    time.Duration
	now        func() time.Time
}

func newIngestRateLimiter() *ingestRateLimiter {
	return newIngestRateLimiterWithOptions(ingestRateLimiterOptions{})
}

func newIngestRateLimiterWithOptions(opts ingestRateLimiterOptions) *ingestRateLimiter {
	if opts.rate <= 0 {
		opts.rate = ingestRate
	}
	if opts.burst <= 0 {
		opts.burst = ingestBurst
	}
	if opts.maxEntries <= 0 {
		opts.maxEntries = defaultIngestRateMaxEntries
	}
	if opts.idleTTL <= 0 {
		opts.idleTTL = defaultIngestRateIdleTTL
	}
	if opts.now == nil {
		opts.now = time.Now
	}
	return &ingestRateLimiter{
		entries:    make(map[string]ingestRateEntry),
		rate:       opts.rate,
		burst:      opts.burst,
		maxEntries: opts.maxEntries,
		idleTTL:    opts.idleTTL,
		now:        opts.now,
	}
}

func (l *ingestRateLimiter) allow(key string) (bool, int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.evictIdleLocked(now)

	entry, ok := l.entries[key]
	if !ok {
		l.makeCapacityLocked()
		entry = ingestRateEntry{tokens: l.burst, updated: now}
	}
	entry.tokens += now.Sub(entry.updated).Seconds() * l.rate
	if entry.tokens > l.burst {
		entry.tokens = l.burst
	}
	entry.updated = now
	if entry.tokens >= 1 {
		entry.tokens--
		l.entries[key] = entry
		return true, 0
	}
	l.entries[key] = entry
	seconds := int((1-entry.tokens)/l.rate + .999)
	if seconds < 1 {
		seconds = 1
	}
	return false, seconds
}

// evictIdleLocked drops entries that have not been touched for longer than the
// idle TTL, keeping the tracked set bounded across long-lived processes even
// without hitting the hard cap.
func (l *ingestRateLimiter) evictIdleLocked(now time.Time) {
	for key, state := range l.entries {
		if now.Sub(state.updated) >= l.idleTTL {
			delete(l.entries, key)
		}
	}
}

// makeCapacityLocked evicts the least-recently-used entry when the tracked set
// is at the hard cap, mirroring LoginAttemptLimiter.makeSourceCapacityLocked.
func (l *ingestRateLimiter) makeCapacityLocked() {
	if len(l.entries) < l.maxEntries {
		return
	}
	var oldestKey string
	var oldestSeen time.Time
	for key, state := range l.entries {
		if oldestKey == "" || state.updated.Before(oldestSeen) {
			oldestKey = key
			oldestSeen = state.updated
		}
	}
	delete(l.entries, oldestKey)
}

func registerExportAuthRoutes(router *gin.RouterGroup, provider CPAInstanceProvider) {
	if provider == nil {
		return
	}
	registerExportAuthRoutesWithLimiter(router, provider, newIngestRateLimiter())
}

func registerExportAuthRoutesWithLimiter(router *gin.RouterGroup, provider CPAInstanceProvider, limiter *ingestRateLimiter) {
	if provider == nil || limiter == nil {
		return
	}
	identity := router.Group("/export")
	identity.Use(ingestAuthentication(provider, limiter, service.ScopeIdentityTest))
	identity.GET("/identity", func(c *gin.Context) {
		trusted, _ := TrustedIngestIdentity(c)
		c.JSON(http.StatusOK, gin.H{"protocolVersion": protocol.ProtocolVersion, "instance": gin.H{"instanceId": trusted.InstanceID, "displayName": trusted.InstanceName}, "credential": gin.H{"credentialId": trusted.CredentialID, "scopes": trusted.Scopes}, "serverTime": protocolTime(time.Now())})
	})
}

func ingestAuthentication(provider CPAInstanceProvider, limiter *ingestRateLimiter, requiredScope string) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if header == "" {
			writeProtocolError(c, "missing_credential")
			c.Abort()
			return
		}
		if !strings.HasPrefix(header, "Bearer ") || strings.Count(header, " ") != 1 || strings.TrimPrefix(header, "Bearer ") == "" {
			writeProtocolError(c, "invalid_credential")
			c.Abort()
			return
		}
		token := strings.TrimPrefix(header, "Bearer ")
		authenticated, err := provider.Authenticate(c.Request.Context(), token)
		if err != nil {
			if err == service.ErrInstanceDisabled {
				writeProtocolError(c, "instance_disabled")
			} else {
				writeProtocolError(c, "invalid_credential")
			}
			c.Abort()
			return
		}
		if ok, retry := limiter.allow(authenticated.Credential.ID); !ok {
			c.Header("Retry-After", strconv.Itoa(retry))
			writeProtocolError(c, "rate_limited")
			c.Abort()
			return
		}
		if !service.HasScope(authenticated.Scopes, requiredScope) {
			writeProtocolError(c, "insufficient_scope")
			c.Abort()
			return
		}
		identity := IngestIdentity{InstanceID: authenticated.Instance.ID, InstanceName: authenticated.Instance.DisplayName, CredentialID: authenticated.Credential.ID, Scopes: service.CanonicalScopes(authenticated.Scopes)}
		c.Set(ingestContextKey, identity)
		c.Request = c.Request.WithContext(ContextWithIngestIdentity(c.Request.Context(), identity))
		c.Next()
	}
}

func TrustedIngestIdentity(c *gin.Context) (IngestIdentity, bool) {
	value, ok := c.Get(ingestContextKey)
	if !ok {
		return IngestIdentity{}, false
	}
	identity, ok := value.(IngestIdentity)
	return identity, ok
}
func ContextWithIngestIdentity(ctx context.Context, identity IngestIdentity) context.Context {
	return context.WithValue(ctx, ingestIdentityContextKey{}, identity)
}

type ingestIdentityContextKey struct{}

func IngestIdentityFromContext(ctx context.Context) (IngestIdentity, bool) {
	identity, ok := ctx.Value(ingestIdentityContextKey{}).(IngestIdentity)
	return identity, ok
}

func writeProtocolError(c *gin.Context, code string) {
	status := protocol.HTTPStatusForCode(code)
	if status == 0 {
		status = http.StatusInternalServerError
		code = "internal_error"
	}
	body := protocol.ErrorForCode(code)
	c.JSON(status, gin.H{"protocolVersion": protocol.ProtocolVersion, "error": gin.H{"code": body.Code, "message": body.Message, "retryable": body.Retryable}})
}
