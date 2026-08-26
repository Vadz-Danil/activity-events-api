package handler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/Vadz-Danil/activity-events-api/internal/middleware"
	"github.com/Vadz-Danil/activity-events-api/internal/models"
	"github.com/Vadz-Danil/activity-events-api/internal/response"
	"github.com/Vadz-Danil/activity-events-api/internal/service"
)

type AggregationService interface {
	RunBucket(ctx context.Context, at time.Time, trigger models.RunTrigger) (*models.AggregationRun, error)
	Runs(ctx context.Context, limit int) ([]models.AggregationRun, error)
	Stats(ctx context.Context, q service.StatsQuery) (*service.Stats, error)
}

type Aggregation struct {
	service AggregationService
	log     *zap.Logger
}

func NewAggregation(svc AggregationService, log *zap.Logger) *Aggregation {
	return &Aggregation{service: svc, log: log}
}

func (h *Aggregation) Stats(c *gin.Context) {
	userID, ok := currentUser(c)
	if !ok {
		return
	}

	from, ok := timeQuery(c, "from")
	if !ok {
		return
	}
	to, ok := timeQuery(c, "to")
	if !ok {
		return
	}

	query := service.StatsQuery{UserID: userID, Daily: c.Query("granularity") == "day"}
	if from != nil {
		query.From = *from
	}
	if to != nil {
		query.To = *to
	}

	stats, err := h.service.Stats(c.Request.Context(), query)
	if err != nil {
		h.fail(c, err)
		return
	}

	c.JSON(http.StatusOK, newStatsResponse(stats))
}

func (h *Aggregation) Trigger(c *gin.Context) {
	var req triggerRequest
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		response.Error(c, http.StatusBadRequest, response.CodeValidationFailed,
			"bucket_start must be an RFC 3339 timestamp")
		return
	}

	var at time.Time
	if req.BucketStart != nil {
		at = *req.BucketStart
	}

	run, err := h.service.RunBucket(c.Request.Context(), at, models.TriggerManual)
	if err != nil {
		h.fail(c, err)
		return
	}

	c.JSON(http.StatusAccepted, newRunResponse(*run))
}

func (h *Aggregation) Runs(c *gin.Context) {
	limit := 0
	if raw := c.Query("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			response.Error(c, http.StatusBadRequest, response.CodeValidationFailed, "limit must be a number")
			return
		}
		limit = parsed
	}

	runs, err := h.service.Runs(c.Request.Context(), limit)
	if err != nil {
		h.fail(c, err)
		return
	}

	c.JSON(http.StatusOK, newRunsResponse(runs))
}

func (h *Aggregation) fail(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrBucketNotClosed):
		response.Error(c, http.StatusBadRequest, response.CodeBucketNotClosed,
			"This bucket has not closed yet, only a finished window can be aggregated")

	case errors.Is(err, service.ErrInvalidTimeRange):
		response.Error(c, http.StatusBadRequest, response.CodeValidationFailed, "from must be before to")

	case errors.Is(err, service.ErrStatsRangeTooLarge):
		response.Error(c, http.StatusBadRequest, response.CodeValidationFailed,
			fmt.Sprintf("The requested window is longer than %d days", int(service.MaxStatsRange.Hours()/24)))

	default:
		h.log.Error("unhandled aggregation error",
			zap.String("request_id", middleware.RequestIDFrom(c)),
			zap.String("path", c.FullPath()),
			zap.Error(err),
		)
		response.Error(c, http.StatusInternalServerError, response.CodeInternal, "Internal server error")
	}
}
