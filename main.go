package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"slices"
	"syscall"
	"time"

	"boot.dev/linko/internal/build"
	"boot.dev/linko/internal/linkoerr"
	"boot.dev/linko/internal/store"
	"github.com/lmittmann/tint"
	"github.com/mattn/go-isatty"
	pkgerr "github.com/pkg/errors"
	"gopkg.in/natefinch/lumberjack.v2"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	httpPort := flag.Int("port", 8899, "port to listen on")
	dataDir := flag.String("data", "./data", "directory to store data")
	flag.Parse()

	status := run(ctx, cancel, *httpPort, *dataDir)
	cancel()
	os.Exit(status)
}

func run(ctx context.Context, cancel context.CancelFunc, httpPort int, dataDir string) int {
	env := os.Getenv("ENV")
	hostname, _ := os.Hostname()
	logger, closeLogger, err := initializeLogger(os.Getenv("LINKO_LOG_FILE"))
	logger = logger.With(
		slog.String("git_sha", build.GitSHA),
		slog.String("build_time", build.BuildTime),
		slog.String("env", env),
		slog.String("hostname", hostname),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize logger: %v\n", err)
		return 1
	}
	defer func() {
		if err := closeLogger(); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to close logger: %v\n", err)
		}
	}()

	st, err := store.New(dataDir, logger)
	if err != nil {
		logger.Error(fmt.Sprintf("failed to create store: %v\n", err))
		return 1
	}

	s := newServer(*st, httpPort, cancel, logger)
	serverErrCh := make(chan error, 1)
	go func() {
		serverErrCh <- s.start()
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	logger.Debug("Linko is shutting down")
	if err := s.shutdown(shutdownCtx); err != nil {
		logger.Error(fmt.Sprintf("failed to shutdown server: %v", err))
		return 1
	}
	serverErr := <-serverErrCh
	if serverErr != nil {
		logger.Error(fmt.Sprintf("server error: %v", serverErr))
		return 1
	}
	return 0
}

type closeFunc func() error

func initializeLogger(filename string) (*slog.Logger, closeFunc, error) {
	handlers := []slog.Handler{
		tint.NewHandler(os.Stderr, &tint.Options{
			Level:       slog.LevelDebug,
			ReplaceAttr: replaceAttr,
			NoColor:     !(isatty.IsCygwinTerminal(os.Stderr.Fd()) || isatty.IsTerminal(os.Stderr.Fd())),
		}),
	}
	closers := []closeFunc{}

	if filename != "" {
		logger := &lumberjack.Logger{
			Filename:   filename,
			MaxSize:    1,
			MaxAge:     28,
			MaxBackups: 10,
			LocalTime:  false,
			Compress:   true,
		}

		close := func() error {
			err := logger.Close()
			if err != nil {
				return fmt.Errorf("failed to close logger: %w", err)
			}
			return nil
		}

		handlers = append(handlers, slog.NewJSONHandler(logger, &slog.HandlerOptions{
			ReplaceAttr: replaceAttr,
		}))
		closers = append(closers, close)
	}

	logger := slog.New(slog.NewMultiHandler(handlers...))

	closer := func() error {
		var errs []error
		for _, close := range closers {
			if err := close(); err != nil {
				errs = append(errs, err)
			}
		}
		return errors.Join(errs...)
	}

	return logger, closer, nil
}

type stackTracer interface {
	error
	StackTrace() pkgerr.StackTrace
}

type multiError interface {
	error
	Unwrap() []error
}

var sensitiveKeys = []string{"password", "key", "apikey", "secret", "pin", "creditcardno"}

func replaceAttr(groups []string, a slog.Attr) slog.Attr {
	if slices.Contains(sensitiveKeys, a.Key) {
		return slog.Attr{Key: a.Key, Value: slog.StringValue("[REDACTED]")}
	}

	if a.Key == "error" {
		err, ok := a.Value.Any().(error)
		if !ok {
			return a
		}

		if multiErr, ok := errors.AsType[multiError](err); ok {
			var errAttrGroups []slog.Attr
			for i, err := range multiErr.Unwrap() {
				errAttrGroups = append(errAttrGroups, slog.GroupAttrs(
					fmt.Sprintf("error_%d", i+1), errorAttrs(err)...,
				))
			}
			return slog.GroupAttrs("errors", errAttrGroups...)
		}

		return slog.GroupAttrs("error", errorAttrs(err)...)
	}

	return a
}

func errorAttrs(err error) []slog.Attr {
	attrs := []slog.Attr{{Key: "message", Value: slog.StringValue(err.Error())}}
	attrs = append(attrs, linkoerr.Attrs(err)...)
	if stackErr, ok := errors.AsType[stackTracer](err); ok {
		attrs = append(
			attrs, slog.Attr{
				Key:   "stack_trace",
				Value: slog.StringValue(fmt.Sprintf("%+va", stackErr.StackTrace())),
			},
		)
	}
	return attrs
}
