package handler

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/khangldit/livequiz-kld/internal/auth"
	"github.com/khangldit/livequiz-kld/internal/db"
	"github.com/khangldit/livequiz-kld/internal/game"
	"github.com/khangldit/livequiz-kld/internal/httpx"
)

// Deps gom các dependency để New() không phải nhận một hàng dài tham số.
type Deps struct {
	Pool      *pgxpool.Pool
	Tokens    *auth.TokenMaker
	Games     *game.Manager
	WSOrigins []string
}

type Handler struct {
	pool      *pgxpool.Pool
	q         *db.Queries
	tokens    *auth.TokenMaker
	games     *game.Manager
	wsOrigins []string
}

func New(d Deps) *Handler {
	return &Handler{
		pool:      d.Pool,
		q:         db.New(d.Pool),
		tokens:    d.Tokens,
		games:     d.Games,
		wsOrigins: d.WSOrigins,
	}
}

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	authed := func(f http.HandlerFunc) http.Handler { return h.tokens.Middleware(f) }

	// Public
	mux.HandleFunc("GET /health", h.health)
	mux.HandleFunc("POST /api/auth/register", h.register)
	mux.HandleFunc("POST /api/auth/login", h.login)

	// Quiz (host)
	mux.Handle("GET /api/me", authed(h.me))
	mux.Handle("GET /api/quizzes", authed(h.listQuizzes))
	mux.Handle("POST /api/quizzes", authed(h.createQuiz))
	mux.Handle("GET /api/quizzes/{id}", authed(h.getQuiz))
	mux.Handle("DELETE /api/quizzes/{id}", authed(h.deleteQuiz))

	// Phòng chơi realtime
	mux.Handle("POST /api/rooms", authed(h.createRoom))
	mux.HandleFunc("GET /api/rooms/{pin}", h.roomInfo)
	mux.HandleFunc("GET /ws/host/{pin}", h.hostWS)
	mux.HandleFunc("GET /ws/play/{pin}", h.playWS)

	return mux
}

func (h *Handler) health(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}