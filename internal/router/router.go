package router

import (
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/Vadz-Danil/activity-events-api/internal/config"
	"github.com/Vadz-Danil/activity-events-api/internal/handler"
	"github.com/Vadz-Danil/activity-events-api/internal/metrics"
	"github.com/Vadz-Danil/activity-events-api/internal/middleware"
)

type Deps struct {
	Config  *config.Config
	Logger  *zap.Logger
	Pool    *pgxpool.Pool
	Metrics *metrics.Metrics
	Version string
}

func New(d Deps) *gin.Engine {
	if d.Config.App.IsProduction() {
		gin.SetMode(gin.ReleaseMode)
	}

	engine := gin.New()
	engine.RedirectTrailingSlash = false

	// Порядок важливий: Recovery має бути *всередині* Metrics і Logger,
	// інакше паніка розкручує стек повз них і запит із 500 не потрапляє
	// ні в метрики, ні в лог. Так само робить сам gin у gin.Default().
	engine.Use(
		middleware.RequestID(),
		middleware.Metrics(d.Metrics),
		middleware.Logger(d.Logger, "/healthz", "/readyz", "/metrics"),
		middleware.Recovery(d.Logger),
		middleware.CORS(d.Config.CORS.AllowedOrigins),
	)

	health := handler.NewHealth(d.Pool, d.Version, string(d.Config.App.Mode))
	engine.GET("/healthz", health.Live)
	engine.GET("/readyz", health.Ready)
	engine.GET("/metrics", gin.WrapH(d.Metrics.Handler()))

	if d.Config.App.Mode.RunsAPI() {
		registerAPI(engine, d)
	}

	return engine
}

func registerAPI(engine *gin.Engine, d Deps) {
	v1 := engine.Group("/api/v1")
	_ = v1
}
