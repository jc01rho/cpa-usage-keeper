package httpapi

import (
	"context"
	"errors"
	"net/http"

	"cpa-usage-keeper/internal/ranking"
	"github.com/gin-gonic/gin"
)

type LocalProvider interface {
	Leaderboard(context.Context, ranking.LeaderboardPeriod, ranking.LeaderboardMetric) (ranking.Leaderboard, error)
}

// RegisterLocalRoutes 只挂载本地只读榜单，不复用 Community 的参与和上报动作。
func RegisterLocalRoutes(router gin.IRoutes, provider LocalProvider) {
	router.GET("/ranking/local/leaderboards", func(c *gin.Context) {
		setNoStoreHeaders(c)
		if provider == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "local_ranking_unavailable"})
			return
		}
		query := c.Request.URL.Query()
		if len(query) != 2 || len(query["period"]) != 1 || len(query["metric"]) != 1 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_leaderboard_selection"})
			return
		}
		period := ranking.LeaderboardPeriod(query.Get("period"))
		metric := ranking.LeaderboardMetric(query.Get("metric"))
		if !validPeriod(period) || !validMetric(metric) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_leaderboard_selection"})
			return
		}
		board, err := provider.Leaderboard(c.Request.Context(), period, metric)
		if err != nil {
			if errors.Is(err, ranking.ErrInvalidLeaderboard) {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_leaderboard_selection"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "local_ranking_failed"})
			return
		}
		c.JSON(http.StatusOK, board)
	})
}
