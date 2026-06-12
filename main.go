package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"boot.dev/linko/internal/store"
)

// var logger = log.New(os.Stderr, "DEBUG: ", log.LstdFlags)

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
	logger, closeLogger, err := initializeLogger(os.Getenv("LINKO_LOG_FILE"))
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
		logger.Info(fmt.Sprintf("failed to create store: %v\n", err))
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

	logger.Info("Linko is shutting down")
	if err := s.shutdown(shutdownCtx); err != nil {
		logger.Info(fmt.Sprintf("failed to shutdown server: %v", err))
		return 1
	}
	serverErr := <-serverErrCh
	if serverErr != nil {
		logger.Info(fmt.Sprintf("server error: %v", serverErr))
		return 1
	}
	return 0
}

type closeFunc func() error

func initializeLogger(filename string) (*slog.Logger, closeFunc, error) {
	if filename != "" {
		file, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to open log file: %v", err)
		}
		bufferedFile := bufio.NewWriterSize(file, 8192)
		multiWriter := io.MultiWriter(bufferedFile, os.Stderr)

		close := func() error {
			flushErr := bufferedFile.Flush()
			closeErr := file.Close()

			if flushErr != nil {
				return flushErr
			}
			if closeErr != nil {
				return closeErr
			}
			return nil
		}

		return slog.New(slog.NewTextHandler(multiWriter, nil)), close, nil
	}
	noopClose := func() error { return nil }
	return slog.New(slog.NewTextHandler(os.Stderr, nil)), noopClose, nil
}
