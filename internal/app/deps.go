package app

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/Vadz-Danil/activity-events-api/internal/auth"
	"github.com/Vadz-Danil/activity-events-api/internal/config"
	"github.com/Vadz-Danil/activity-events-api/internal/handler"
	"github.com/Vadz-Danil/activity-events-api/internal/metrics"
	"github.com/Vadz-Danil/activity-events-api/internal/middleware"
	"github.com/Vadz-Danil/activity-events-api/internal/repository"
	"github.com/Vadz-Danil/activity-events-api/internal/router"
	"github.com/Vadz-Danil/activity-events-api/internal/service"
)

func BuildDeps(cfg *config.Config, pool *pgxpool.Pool, m *metrics.Metrics, log *zap.Logger, version string) (router.Deps, error) {
	deps := router.Deps{
		Config:  cfg,
		Logger:  log,
		Pool:    pool,
		Metrics: m,
		Version: version,
	}

	if !cfg.App.Mode.RunsAPI() {
		return deps, nil
	}

	if err := wireAuth(&deps, cfg, pool, log); err != nil {
		return router.Deps{}, err
	}
	return deps, nil
}

func wireAuth(deps *router.Deps, cfg *config.Config, pool *pgxpool.Pool, log *zap.Logger) error {
	jwtManager, err := auth.NewManager(cfg.Auth.JWTSecret, cfg.Auth.Issuer, cfg.Auth.AccessTTL)
	if err != nil {
		return err
	}

	var (
		googleVerifier  service.GoogleVerifier
		googleExchanger service.GoogleExchanger
	)

	switch {
	case cfg.Google.CodeFlowEnabled():
		googleVerifier = auth.NewGoogleVerifier(cfg.Google.ClientID, cfg.Google.JWKSURL)
		googleExchanger = auth.NewGoogleExchanger(
			cfg.Google.ClientID, cfg.Google.ClientSecret, cfg.Google.RedirectURL,
			cfg.Google.AuthURL, cfg.Google.TokenURL,
		)
		log.Info("google sign-in enabled", zap.String("flows", "id-token, authorization code"))
	case cfg.Google.Enabled():
		googleVerifier = auth.NewGoogleVerifier(cfg.Google.ClientID, cfg.Google.JWKSURL)
		log.Info("google sign-in enabled", zap.String("flows", "id-token"))
	default:
		log.Info("google sign-in disabled: GOOGLE_CLIENT_ID is not set")
	}

	authService, err := service.NewAuth(service.AuthDeps{
		Users:      repository.NewUserRepository(pool),
		Tokens:     repository.NewRefreshTokenRepository(pool),
		OAuth:      repository.NewGoogleOAuthRepository(pool),
		JWT:        jwtManager,
		Google:     googleVerifier,
		Exchanger:  googleExchanger,
		Logger:     log,
		BcryptCost: cfg.Auth.BcryptCost,
		RefreshTTL: cfg.Auth.RefreshTTL,
	})
	if err != nil {
		return err
	}

	deps.Auth = handler.NewAuth(authService, log, cfg.App.FrontendURL)
	deps.Guard = middleware.NewGuard(jwtManager)

	return nil
}
