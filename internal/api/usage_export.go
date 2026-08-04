package api

import (
	"io"
	"net/http"
	"time"

	"cpa-usage-keeper/internal/protocol"
	"cpa-usage-keeper/internal/service"
	"github.com/gin-gonic/gin"
)

const usageExportReadTimeout = 15 * time.Second

// TrustedUsageIngestContext is the narrow Task 5 consumer view of trusted
// ingest authentication. Task 4 middleware supplies it from credential-bound
// context; usage handlers never inspect tokens or accept body instance IDs.
type TrustedUsageIngestContext interface {
	TrustedUsageIdentity(*gin.Context) (IngestIdentity, bool)
}

type ginUsageIngestContext struct{}

func (ginUsageIngestContext) TrustedUsageIdentity(c *gin.Context) (IngestIdentity, bool) {
	return TrustedIngestIdentity(c)
}

func registerUsageExportRoutes(router *gin.RouterGroup, provider service.UsageExportProvider, authProvider CPAInstanceProvider) {
	if provider == nil || authProvider == nil {
		return
	}
	limiter := newIngestRateLimiter()
	export := router.Group("/export")
	export.Use(ingestAuthentication(authProvider, limiter, service.ScopeUsagePush))
	contextAccessor := ginUsageIngestContext{}
	handler := func(c *gin.Context) {
		trusted, ok := contextAccessor.TrustedUsageIdentity(c)
		if !ok || trusted.InstanceID == "" {
			writeProtocolError(c, "invalid_credential")
			return
		}
		if !validUsageExportHeaders(c.Request) {
			writeProtocolError(c, "invalid_field")
			return
		}
		controller := http.NewResponseController(c.Writer)
		deadlineSet := controller.SetReadDeadline(time.Now().Add(usageExportReadTimeout)) == nil
		defer func() {
			if c.Request.Body != nil {
				_ = c.Request.Body.Close()
			}
			if deadlineSet {
				_ = controller.SetReadDeadline(time.Time{})
			}
		}()
		if c.Request.ContentLength > protocol.MaxBodyBytes {
			writeProtocolError(c, "request_too_large")
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, protocol.MaxBodyBytes))
		if err != nil {
			if isRequestEntityTooLarge(err) {
				writeProtocolError(c, "request_too_large")
				return
			}
			writeProtocolError(c, "invalid_json")
			return
		}
		batch, perr := protocol.DecodeUsageBatch(body)
		if perr != nil {
			writeProtocolError(c, perr.Code)
			return
		}
		ack, perr := provider.IngestUsageBatch(c.Request.Context(), trusted.InstanceID, batch)
		if perr != nil {
			writeProtocolError(c, perr.Code)
			return
		}
		c.Header("Content-Type", "application/json; charset=utf-8")
		c.JSON(http.StatusOK, gin.H{
			"protocolVersion":      protocol.ProtocolVersion,
			"streamId":             ack.StreamID,
			"acknowledgedThrough":  ack.AcknowledgedThrough,
			"nextExpectedSequence": ack.NextExpectedSequence,
			"acceptedCount":        ack.AcceptedCount,
			"replayedCount":        ack.ReplayedCount,
		})
	}
	export.POST("/usage", handler)
	export.POST("/usage-batches", handler)
}

func validUsageExportHeaders(request *http.Request) bool {
	if request.Header.Get("Content-Encoding") != "" {
		return false
	}
	contentType := request.Header.Get("Content-Type")
	return contentType == "application/json" || contentType == "application/json; charset=utf-8"
}
