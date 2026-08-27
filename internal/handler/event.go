package handler

import (
	"context"
	"encoding/json"
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

const (
	idempotencyHeader = "Idempotency-Key"

	streamHeartbeat  = 20 * time.Second
	streamMediaType  = "text/event-stream"
	streamEventName  = "activity"
	streamEventFrame = "id: %d\nevent: %s\ndata: %s\n\n"
	streamKeepAlive  = ": keep-alive\n\n"
)

type EventService interface {
	Record(ctx context.Context, userID int64, in service.EventInput) (*models.Event, bool, error)
	RecordBatch(ctx context.Context, userID int64, in []service.EventInput) (*service.BatchResult, error)
	List(ctx context.Context, q service.EventQuery) (*service.EventPage, error)
}

type EventStream interface {
	Subscribe(userID int64) (<-chan models.Event, func())
}

type Event struct {
	service EventService
	stream  EventStream
	log     *zap.Logger
}

func NewEvent(svc EventService, stream EventStream, log *zap.Logger) *Event {
	return &Event{service: svc, stream: stream, log: log}
}

func (h *Event) Stream(c *gin.Context) {
	userID, ok := currentUser(c)
	if !ok {
		return
	}

	events, cancel := h.stream.Subscribe(userID)
	defer cancel()

	writer := http.NewResponseController(c.Writer)
	if err := writer.SetWriteDeadline(time.Time{}); err != nil {
		h.log.Debug("stream write deadline is left as configured", zap.Error(err))
	}

	c.Header("Content-Type", streamMediaType)
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)
	c.Writer.Flush()

	heartbeat := time.NewTicker(streamHeartbeat)
	defer heartbeat.Stop()

	ctx := c.Request.Context()
	for {
		select {
		case <-ctx.Done():
			return

		case event, open := <-events:
			if !open {
				return
			}

			payload, err := json.Marshal(newEventResponse(event))
			if err != nil {
				h.log.Error("encode streamed event", zap.Int64("event_id", event.ID), zap.Error(err))
				continue
			}

			if !h.write(c, fmt.Sprintf(streamEventFrame, event.ID, streamEventName, payload)) {
				return
			}

		case <-heartbeat.C:
			if !h.write(c, streamKeepAlive) {
				return
			}
		}
	}
}

func (h *Event) write(c *gin.Context, chunk string) bool {
	if _, err := io.WriteString(c.Writer, chunk); err != nil {
		h.log.Debug("stream closed by the client",
			zap.String("request_id", middleware.RequestIDFrom(c)),
			zap.Error(err),
		)
		return false
	}

	c.Writer.Flush()
	return true
}

func (h *Event) Create(c *gin.Context) {
	userID, ok := currentUser(c)
	if !ok {
		return
	}

	var req eventRequest
	if !bindJSON(c, &req) {
		return
	}
	if req.IdempotencyKey == "" {
		req.IdempotencyKey = c.GetHeader(idempotencyHeader)
	}

	event, created, err := h.service.Record(c.Request.Context(), userID, req.toInput())
	if err != nil {
		h.fail(c, err)
		return
	}

	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	c.JSON(status, newEventResponse(*event))
}

func (h *Event) CreateBatch(c *gin.Context) {
	userID, ok := currentUser(c)
	if !ok {
		return
	}

	var req batchRequest
	if !bindJSON(c, &req) {
		return
	}

	inputs := make([]service.EventInput, 0, len(req.Events))
	for _, item := range req.Events {
		inputs = append(inputs, item.toInput())
	}

	result, err := h.service.RecordBatch(c.Request.Context(), userID, inputs)
	if err != nil {
		h.fail(c, err)
		return
	}

	c.JSON(http.StatusCreated, newBatchResponse(result))
}

func (h *Event) List(c *gin.Context) {
	userID, ok := currentUser(c)
	if !ok {
		return
	}

	target, ok := targetUser(c, userID)
	if !ok {
		return
	}

	query := service.EventQuery{
		UserID: target,
		Types:  c.QueryArray("type"),
		Cursor: c.Query("cursor"),
	}

	if raw := c.Query("limit"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil {
			response.Error(c, http.StatusBadRequest, response.CodeValidationFailed, "limit must be a number")
			return
		}
		query.Limit = limit
	}

	from, ok := timeQuery(c, "from")
	if !ok {
		return
	}
	to, ok := timeQuery(c, "to")
	if !ok {
		return
	}
	query.From, query.To = from, to

	page, err := h.service.List(c.Request.Context(), query)
	if err != nil {
		h.fail(c, err)
		return
	}

	c.JSON(http.StatusOK, newEventPageResponse(target, page))
}

func (h *Event) fail(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrInvalidEventType):
		h.badRequest(c, response.CodeValidationFailed, "Event type must not be empty and must be at most 64 characters")

	case errors.Is(err, service.ErrInvalidEventPayload):
		h.badRequest(c, response.CodeValidationFailed, "Event payload must be a json object of at most 8 KB")

	case errors.Is(err, service.ErrEventTimeOutOfRange):
		h.badRequest(c, response.CodeValidationFailed, "occurred_at is too far in the future or in the past")

	case errors.Is(err, service.ErrInvalidIdempotencyKey):
		h.badRequest(c, response.CodeValidationFailed, "Idempotency key is too long")

	case errors.Is(err, service.ErrDuplicateIdempotencyKey):
		h.badRequest(c, response.CodeValidationFailed, "The same idempotency key is used twice in one batch")

	case errors.Is(err, service.ErrEmptyBatch):
		h.badRequest(c, response.CodeValidationFailed, "The batch has no events")

	case errors.Is(err, service.ErrBatchTooLarge):
		h.badRequest(c, response.CodeValidationFailed, "The batch holds more events than the limit allows")

	case errors.Is(err, service.ErrInvalidCursor):
		h.badRequest(c, response.CodeInvalidCursor, "Pagination cursor is invalid")

	case errors.Is(err, service.ErrInvalidTimeRange):
		h.badRequest(c, response.CodeValidationFailed, "from must be before to")

	default:
		h.log.Error("unhandled event error",
			zap.String("request_id", middleware.RequestIDFrom(c)),
			zap.String("path", c.FullPath()),
			zap.Error(err),
		)
		response.Error(c, http.StatusInternalServerError, response.CodeInternal, "Internal server error")
	}
}

func (h *Event) badRequest(c *gin.Context, code, message string) {
	response.Error(c, http.StatusBadRequest, code, message)
}

func currentUser(c *gin.Context) (int64, bool) {
	userID, ok := middleware.UserIDFrom(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, response.CodeUnauthorized,
			"Authorization header with a bearer token is required")
		return 0, false
	}
	return userID, true
}

func targetUser(c *gin.Context, self int64) (int64, bool) {
	raw := c.Query("user_id")
	if raw == "" {
		return self, true
	}

	requested, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || requested <= 0 {
		response.Error(c, http.StatusBadRequest, response.CodeValidationFailed,
			"user_id must be a positive number")
		return 0, false
	}
	if requested == self {
		return self, true
	}

	if role, _ := middleware.RoleFrom(c); role != models.RoleAdmin {
		response.Error(c, http.StatusForbidden, response.CodeForbidden,
			"Reading another user's activity requires the admin role")
		return 0, false
	}

	return requested, true
}

func timeQuery(c *gin.Context, key string) (*time.Time, bool) {
	raw := c.Query(key)
	if raw == "" {
		return nil, true
	}

	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		response.Error(c, http.StatusBadRequest, response.CodeValidationFailed,
			key+" must be an RFC 3339 timestamp")
		return nil, false
	}

	return &parsed, true
}
