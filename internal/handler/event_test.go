package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/Vadz-Danil/activity-events-api/internal/auth"
	"github.com/Vadz-Danil/activity-events-api/internal/middleware"
	"github.com/Vadz-Danil/activity-events-api/internal/models"
	"github.com/Vadz-Danil/activity-events-api/internal/response"
	"github.com/Vadz-Danil/activity-events-api/internal/service"
)

const (
	testSelfID  int64 = 7
	testOtherID int64 = 42
)

func TestReadScopeIsLimitedToOwnDataUnlessAdmin(t *testing.T) {
	tests := []struct {
		name   string
		role   models.Role
		query  string
		status int
		want   int64
	}{
		{"without user_id reads own data", models.RoleUser, "", http.StatusOK, testSelfID},
		{"own user_id needs no role", models.RoleUser, "?user_id=7", http.StatusOK, testSelfID},
		{"admin reads another user", models.RoleAdmin, "?user_id=42", http.StatusOK, testOtherID},
		{"a regular user is refused", models.RoleUser, "?user_id=42", http.StatusForbidden, 0},
		{"user_id must be a number", models.RoleAdmin, "?user_id=abc", http.StatusBadRequest, 0},
		{"user_id must be positive", models.RoleAdmin, "?user_id=0", http.StatusBadRequest, 0},
		{"user_id must not be negative", models.RoleAdmin, "?user_id=-1", http.StatusBadRequest, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, events, stats, token := scopedEngine(t)

			t.Run("events", func(t *testing.T) {
				rec := do(engine, http.MethodGet, "/events"+tt.query, token(testSelfID, tt.role))

				require.Equal(t, tt.status, rec.Code, rec.Body.String())
				if tt.status != http.StatusOK {
					require.False(t, events.called, "the service must not be reached on a refused request")
					return
				}
				require.Equal(t, tt.want, events.query.UserID)
			})

			t.Run("stats", func(t *testing.T) {
				rec := do(engine, http.MethodGet, "/stats"+tt.query, token(testSelfID, tt.role))

				require.Equal(t, tt.status, rec.Code, rec.Body.String())
				if tt.status != http.StatusOK {
					require.False(t, stats.called, "the service must not be reached on a refused request")
					return
				}
				require.Equal(t, tt.want, stats.query.UserID)
			})
		})
	}
}

func TestRefusedReadReportsForbidden(t *testing.T) {
	engine, _, _, token := scopedEngine(t)

	rec := do(engine, http.MethodGet, "/events?user_id=42", token(testSelfID, models.RoleUser))

	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Equal(t, "forbidden", errorCode(t, rec))
}

func scopedEngine(t *testing.T) (*gin.Engine, *fakeEventService, *fakeStatsService, func(int64, models.Role) string) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	manager, err := auth.NewManager("secret-long-enough-for-hs256-signing", "test", 15*time.Minute)
	require.NoError(t, err)

	events := &fakeEventService{}
	stats := &fakeStatsService{}
	guard := middleware.NewGuard(manager)

	engine := gin.New()
	engine.GET("/events", guard.RequireAuth(), NewEvent(events, nopStream{}, zap.NewNop()).List)
	engine.GET("/stats", guard.RequireAuth(), NewAggregation(stats, zap.NewNop()).Stats)

	token := func(id int64, role models.Role) string {
		access, err := manager.Issue(id, role, time.Now())
		require.NoError(t, err)
		return "Bearer " + access.Token
	}

	return engine, events, stats, token
}

func do(engine *gin.Engine, method, path, authHeader string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Authorization", authHeader)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()

	var body response.ErrorEnvelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	return body.Error.Code
}

type fakeEventService struct {
	called bool
	query  service.EventQuery
}

func (f *fakeEventService) Record(context.Context, int64, service.EventInput) (*models.Event, bool, error) {
	return nil, false, nil
}

func (f *fakeEventService) RecordBatch(context.Context, int64, []service.EventInput) (*service.BatchResult, error) {
	return nil, nil
}

func (f *fakeEventService) List(_ context.Context, q service.EventQuery) (*service.EventPage, error) {
	f.called, f.query = true, q
	return &service.EventPage{}, nil
}

type fakeStatsService struct {
	called bool
	query  service.StatsQuery
}

func (f *fakeStatsService) RunBucket(context.Context, time.Time, models.RunTrigger) (*models.AggregationRun, error) {
	return &models.AggregationRun{}, nil
}

func (f *fakeStatsService) Runs(context.Context, int) ([]models.AggregationRun, error) {
	return nil, nil
}

func (f *fakeStatsService) Stats(_ context.Context, q service.StatsQuery) (*service.Stats, error) {
	f.called, f.query = true, q
	return &service.Stats{Bucket: models.BucketDuration}, nil
}

type nopStream struct{}

func (nopStream) Subscribe(int64) (<-chan models.Event, func()) {
	return make(chan models.Event), func() {}
}
