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
	Auth    *handler.Auth
	Guard   *middleware.Guard
}

func New(d Deps) *gin.Engine {
	if d.Config.App.IsProduction() {
		gin.SetMode(gin.ReleaseMode)
	}

	engine := gin.New()
	engine.RedirectTrailingSlash = false

	if err := engine.SetTrustedProxies(d.Config.HTTP.TrustedProxies); err != nil {
		d.Logger.Error("failed to set trusted proxies", zap.Error(err))
	}

	engine.Use(
		middleware.RequestID(),
		middleware.Metrics(d.Metrics),
		middleware.Logger(d.Logger, "/healthz", "/readyz", "/metrics"),
		middleware.Recovery(d.Logger),
		middleware.CORS(d.Config.CORS.AllowedOrigins),
		middleware.BodyLimit(d.Config.HTTP.MaxBodyBytes),
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

	if d.Auth == nil || d.Guard == nil {
		d.Logger.Warn("auth is not wired, /auth endpoints are not registered")
		return
	}

	authRoutes := v1.Group("/auth")
	authRoutes.POST("/register", d.Auth.Register)
	authRoutes.POST("/login", d.Auth.Login)
	authRoutes.GET("/google/start", d.Auth.GoogleStart)
	authRoutes.GET("/google/callback", d.Auth.GoogleCallback)
	authRoutes.POST("/google/exchange", d.Auth.GoogleExchange)
	authRoutes.POST("/refresh", d.Auth.Refresh)
	authRoutes.POST("/logout", d.Auth.Logout)
	authRoutes.GET("/me", d.Guard.RequireAuth(), d.Auth.Me)
}
