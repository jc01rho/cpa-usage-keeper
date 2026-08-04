package api

import (
	"encoding/hex"
	"net/http"

	"cpa-usage-keeper/internal/service"
	"cpa-usage-keeper/internal/timeutil"
	"github.com/gin-gonic/gin"
)

func registerMetadataStatusRoutes(router gin.IRoutes, provider service.MetadataStatusProvider) {
	router.GET("/instances/metadata-status", func(c *gin.Context) {
		if provider == nil {
			c.JSON(http.StatusOK, gin.H{"items": []any{}})
			return
		}
		rows, err := provider.ListMetadataStatus(c.Request.Context(), instanceFilterFromGin(c).InstanceID)
		if err != nil {
			writeInternalError(c, "list metadata status failed", err)
			return
		}
		items := make([]gin.H, 0, len(rows))
		for _, row := range rows {
			items = append(items, gin.H{
				"instanceId":   row.InstanceID,
				"instanceName": row.InstanceName,
				"category":     row.Category,
				"revision":     row.Revision,
				"digest":       hex.EncodeToString(row.BodyDigest),
				"itemCount":    row.ItemCount,
				"generatedAt":  timeutil.FormatStorageTime(row.GeneratedAt),
				"appliedAt":    timeutil.FormatStorageTime(row.AppliedAt),
				"lastError":    row.LastError,
			})
		}
		c.JSON(http.StatusOK, gin.H{"items": items})
	})
}
