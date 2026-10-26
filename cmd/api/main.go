package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"

	"github.com/khangldit/livequiz-kld/internal/auth"
	"github.com/khangldit/livequiz-kld/internal/config"
	"github.com/khangldit/livequiz-kld/internal/game"
	"github.com/khangldit/livequiz-kld/internal/handler"
	"github.com/khangldit/livequiz-kld/internal/httpx"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	_ = godotenv.Load() // chỉ có tác dụng ở local; trên Render biến môi trường đã có sẵn
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return err
	}
	slog.Info("database connected")

	tokens := auth.NewTokenMaker(cfg.JWTSecret)
	games := game.NewManager(nil) // nil = chưa lưu kết quả (Phần 3 sẽ thêm)
	h := handler.New(handler.Deps{
		Pool:      pool,
		Tokens:    tokens,
		Games:     games,
		WSOrigins: wsOrigins(cfg.CORSOrigin),
	})

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           httpx.CORS(cfg.CORSOrigin, h.Routes()),
		ReadHeaderTimeout: 5 * time.Second,
		// Không đặt WriteTimeout: nó sẽ cắt các kết nối WebSocket sống lâu.
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("server starting", "port", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	slog.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// srv.Shutdown không quản lý kết nối WebSocket (đã bị "hijack") → phải tự đóng các phòng.
	return errors.Join(
		srv.Shutdown(shutdownCtx),
		games.Shutdown(shutdownCtx),
	)
}

// wsOrigins: "https://livequiz.vercel.app" → ["livequiz.vercel.app"]
// (thư viện websocket so khớp theo host của header Origin).
func wsOrigins(corsOrigin string) []string {
	u, err := url.Parse(corsOrigin)
	if err != nil || u.Host == "" {
		return nil
	}
	return []string{u.Host}
}