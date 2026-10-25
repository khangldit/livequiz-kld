package handler

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/khangldit/livequiz-kld/internal/auth"
	"github.com/khangldit/livequiz-kld/internal/db"
	"github.com/khangldit/livequiz-kld/internal/httpx"
)

type Handler struct {
	pool   *pgxpool.Pool
	q      *db.Queries
	tokens *auth.TokenMaker
}

func New(pool *pgxpool.Pool, tokens *auth.TokenMaker) *Handler {
	return &Handler{pool: pool, q: db.New(pool), tokens: tokens}
}

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	authed := func(f http.HandlerFunc) http.Handler { return h.tokens.Middleware(f) }

	// Public
	mux.HandleFunc("GET /health", h.health)
	mux.HandleFunc("POST /api/auth/register", h.register)
	mux.HandleFunc("POST /api/auth/login", h.login)

	// Cần đăng nhập
	mux.Handle("GET /api/me", authed(h.me))

	mux.Handle("GET /api/quizzes", authed(h.listQuizzes))
	mux.Handle("POST /api/quizzes", authed(h.createQuiz))
	mux.Handle("GET /api/quizzes/{id}", authed(h.getQuiz))
	mux.Handle("DELETE /api/quizzes/{id}", authed(h.deleteQuiz))

	return mux
}

func (h *Handler) health(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}