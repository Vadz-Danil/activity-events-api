package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Health struct {
	pool    *pgxpool.Pool
	version string
	mode    string
}

func NewHealth(pool *pgxpool.Pool, version, mode string) *Health {
	return &Health{pool: pool, version: version, mode: mode}
}

func (h *Health) Live(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"version": h.version,
		"mode":    h.mode,
	})
}

func (h *Health) Ready(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()

	if err := h.pool.Ping(ctx); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status":   "unavailable",
			"database": "down",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":   "ok",
		"database": "up",
	})
}
