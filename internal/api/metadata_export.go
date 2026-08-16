package api

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"cpa-usage-keeper/internal/protocol"
	"cpa-usage-keeper/internal/service"
	"github.com/gin-gonic/gin"
)

type TrustedMetadataIngestContext interface {
	TrustedMetadataIdentity(*gin.Context) (IngestIdentity, bool)
}

type ginMetadataIngestContext struct{}

type metadataIngestAuthProvider struct {
	CPAInstanceProvider
}

func (p metadataIngestAuthProvider) Authenticate(ctx context.Context, token string) (service.AuthenticatedIngestCredential, error) {
	return p.CPAInstanceProvider.Authenticate(ctx, token)
}

func (ginMetadataIngestContext) TrustedMetadataIdentity(c *gin.Context) (IngestIdentity, bool) {
	return TrustedIngestIdentity(c)
}

func registerMetadataExportRoutes(router *gin.RouterGroup, provider service.MetadataExportProvider, authProvider CPAInstanceProvider) {
	if provider == nil || authProvider == nil {
		return
	}
	limiter := newIngestRateLimiter()
	routes := map[string]protocol.MetadataCategory{"/auth_files": protocol.CategoryAuthFiles, "/api_keys": protocol.CategoryAPIKeys, "/provider_identities": protocol.CategoryProviderIdentities}
	export := router.Group("/export/metadata")
	export.Use(rejectMetadataRequestInstanceSelectors())
	for path := range routes {
		export.PUT(path+"/:instanceSelector", func(c *gin.Context) {
			writeProtocolError(c, "body_instance_forbidden")
		})
	}
	export.Use(ingestAuthentication(authProvider, limiter, service.ScopeMetadataPush))
	accessor := ginMetadataIngestContext{}
	for path, category := range routes {
		category := category
		export.PUT(path, func(c *gin.Context) {
			trusted, ok := accessor.TrustedMetadataIdentity(c)
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
				} else {
					writeProtocolError(c, "invalid_json")
				}
				return
			}
			snapshot, perr := protocol.DecodeMetadataSnapshot(body, category)
			if perr != nil {
				writeProtocolError(c, perr.Code)
				return
			}
			result, perr := provider.IngestMetadataSnapshot(c.Request.Context(), trusted.InstanceID, category, snapshot, body)
			if perr != nil {
				if perr.CurrentRevision > 0 {
					c.Header(protocol.RevisionHintHeader, strconv.FormatInt(perr.CurrentRevision, 10))
				}
				writeProtocolError(c, perr.Code)
				return
			}
			c.Header("Content-Type", "application/json; charset=utf-8")
			c.JSON(http.StatusOK, gin.H{"protocolVersion": protocol.ProtocolVersion, "category": result.Category, "revision": result.Revision, "applied": result.Applied, "itemCount": result.ItemCount, "serverTime": result.ServerTime})
		})
	}
}

func rejectMetadataRequestInstanceSelectors() gin.HandlerFunc {
	return func(c *gin.Context) {
		for key := range c.Request.URL.Query() {
			if isInstanceSelectorName(key) {
				writeProtocolError(c, "body_instance_forbidden")
				c.Abort()
				return
			}
		}
		for key := range c.Request.Header {
			if isInstanceSelectorName(key) {
				writeProtocolError(c, "body_instance_forbidden")
				c.Abort()
				return
			}
		}
		if c.Request.ContentLength > protocol.MaxBodyBytes {
			c.Next()
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, protocol.MaxBodyBytes))
		if err != nil {
			if isRequestEntityTooLarge(err) {
				writeProtocolError(c, "request_too_large")
			} else {
				writeProtocolError(c, "invalid_json")
			}
			c.Abort()
			return
		}
		_ = c.Request.Body.Close()
		c.Request.Body = io.NopCloser(bytes.NewReader(body))
		c.Request.ContentLength = int64(len(body))
		if perr := protocol.RejectInstanceSelectors(body); perr != nil {
			writeProtocolError(c, perr.Code)
			c.Abort()
			return
		}
		c.Next()
	}
}

func isInstanceSelectorName(name string) bool {
	var normalized strings.Builder
	for _, character := range strings.ToLower(strings.TrimSpace(name)) {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' {
			normalized.WriteRune(character)
		}
	}
	return normalized.String() == "instanceid"
}
