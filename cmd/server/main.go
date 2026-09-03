package main

import (
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/go-kratos/kratos/contrib/otel/v3/tracing"
	"github.com/go-kratos/kratos/v3/config"
	"github.com/go-kratos/kratos/v3/config/env"
	"github.com/go-kratos/kratos/v3/config/file"
	"github.com/go-kratos/kratos/v3/log"

	"github.com/zhangzhe-ctrl/ani-iam/internal/conf"

	"go.uber.org/automaxprocs/maxprocs"
)

// These values can be overridden with -ldflags at build time.
var (
	Name     = "ani-iam-cp0"
	Version  = "cp0.0"
	flagconf string
	id, _    = os.Hostname()
)

func init() {
	flag.StringVar(&flagconf, "conf", "configs", "config path, for example -conf configs/config.yaml")
}

func main() {
	flag.Parse()
	logger := newRuntimeLogger(os.Stdout)
	log.SetDefault(logger)
	undoMaxProcs, err := maxprocs.Set(maxprocs.Logger(func(format string, args ...interface{}) {
		log.Info("runtime CPU quota", "detail", fmt.Sprintf(format, args...))
	}))
	if err != nil {
		panic(err)
	}
	defer undoMaxProcs()

	c := config.New(config.WithSource(
		file.NewSource(flagconf),
		env.NewSource("KRATOS"),
	))
	defer c.Close()
	if err := c.Load(); err != nil {
		panic(err)
	}

	var bc conf.Bootstrap
	if err := c.Scan(&bc); err != nil {
		panic(err)
	}
	app, err := buildApp(&bc, logger)
	if err != nil {
		panic(err)
	}
	if err := app.Run(); err != nil {
		panic(err)
	}
}

func newRuntimeLogger(writer io.Writer) *slog.Logger {
	handler := log.NewHandler(
		log.WithWriter(writer),
		log.WithFormat(log.FormatJSON),
		log.WithLevel(log.LevelInfo),
		log.WithAddSource(true),
		log.WithExtractor(tracing.TraceAttrs),
		log.WithFilter(log.FilterKey(
			"args",
			"authorization",
			"cookie",
			"credential",
			"password",
			"private_key",
			"set-cookie",
			"token",
		)),
	)
	return slog.New(handler).With(
		slog.String("service.id", id),
		slog.String("service.name", Name),
		slog.String("service.version", Version),
	)
}
