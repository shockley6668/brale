//go:generate go run -mod=mod github.com/google/wire/cmd/wire
//go:build !wireinject

package app

import (
	"brale/internal/config"
	"context"
)

func buildAppWithWire(ctx context.Context, cfg *config.Config) (*App, error) {
	appBuilder := provideAppBuilder(cfg)
	app, err := provideAppFromBuilder(appBuilder, ctx)
	if err != nil {
		return nil, err
	}
	return app, nil
}

type appBuilderDeps interface {
	Build(context.Context) (*App, error)
}

func provideAppFromBuilder(b appBuilderDeps, ctx context.Context) (*App, error) {
	return b.Build(ctx)
}

func provideAppBuilder(cfg *config.Config) *AppBuilder {
	return NewAppBuilder(cfg)
}
