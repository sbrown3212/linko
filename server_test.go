package main

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func Test_requestLogger(t *testing.T) {
	logBuffer := &bytes.Buffer{}

	logger := slog.New(slog.NewTextHandler(logBuffer, &slog.HandlerOptions{
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				return slog.Time(slog.TimeKey, time.Date(2023, 10, 1, 12, 34, 57, 0, time.UTC))
			}
			return a
		},
	}))

	requestLoggerMiddleware := requestLogger(logger)

	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

	loggedHandler := requestLoggerMiddleware(dummyHandler)

	req := httptest.NewRequest("GET", "http://localhost:8080/test?foo=bar", nil)

	rr := httptest.NewRecorder()

	loggedHandler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status code: 200, got: %d", rr.Code)
	}

	logOutput := logBuffer.String()

	if !strings.Contains(logOutput, `msg="Served request"`) {
		t.Errorf("expected log to contain served request message, got:\n%s", logOutput)
	}

	if !strings.Contains(logOutput, `path=/test`) {
		t.Errorf("expected log to contain path=/test, got:\n%s", logOutput)
	}

	if !strings.Contains(logOutput, `client_ip=192.0.2.x`) {
		t.Errorf("expected log to contain path=/test, got:\n%s", logOutput)
	}
}
