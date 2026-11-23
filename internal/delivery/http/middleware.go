package http

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"jlptmaster/backend/internal/usecase"

	"github.com/gin-gonic/gin"
)

func CORSMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, PATCH, DELETE")

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

func LatencyHeaderMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		elapsedMs := float64(time.Since(start).Microseconds()) / 1000.0
		c.Writer.Header().Set("X-SQLite-Latency-Ms", fmt.Sprintf("%.2f", elapsedMs))
	}
}

// OfflineFirstAuthMiddleware validates the JWT Bearer token if present; if absent or in offline local mode,
// seamlessly binds to the default local study profile (user_id = 1) so offline study never blocks.
func OfflineFirstAuthMiddleware(authUC *usecase.AuthUsecase) gin.HandlerFunc {
	return func(c *gin.Context) {
		var userID int64 = 1
		authHeader := c.GetHeader("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			token := strings.TrimPrefix(authHeader, "Bearer ")
			if id, err := authUC.ValidateToken(token); err == nil && id > 0 {
				userID = id
			}
		}
		c.Set("user_id", userID)
		c.Next()
	}
}

func getUserID(c *gin.Context) int64 {
	if val, exists := c.Get("user_id"); exists {
		if id, ok := val.(int64); ok && id > 0 {
			return id
		}
	}
	return 1
}
