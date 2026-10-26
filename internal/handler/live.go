package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/jackc/pgx/v5"

	"github.com/khangldit/livequiz-kld/internal/auth"
	"github.com/khangldit/livequiz-kld/internal/db"
	"github.com/khangldit/livequiz-kld/internal/game"
	"github.com/khangldit/livequiz-kld/internal/httpx"
)

type createRoomRequest struct {
	QuizID int64 `json:"quizId"`
}

// POST /api/rooms — host mở phòng chơi từ 1 quiz của mình.
func (h *Handler) createRoom(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID := auth.UserID(ctx)

	var req createRoomRequest
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	quiz, err := h.q.GetQuiz(ctx, db.GetQuizParams{ID: req.QuizID, OwnerID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Error(w, http.StatusNotFound, "quiz not found")
		return
	}
	if err != nil {
		slog.Error("get quiz", "err", err)
		httpx.Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	questions, err := h.q.ListQuestionsByQuiz(ctx, quiz.ID)
	if err != nil {
		slog.Error("list questions", "err", err)
		httpx.Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	if len(questions) == 0 {
		httpx.Error(w, http.StatusBadRequest, "quiz has no questions")
		return
	}

	// Chuyển từ model DB sang model game (package game không biết gì về DB).
	gq := game.Quiz{ID: quiz.ID, Title: quiz.Title}
	for _, q := range questions {
		gq.Questions = append(gq.Questions, game.Question{
			Text:         q.Text,
			Options:      q.Options,
			CorrectIndex: int(q.CorrectIndex),
			TimeLimit:    time.Duration(q.TimeLimitSec) * time.Second,
		})
	}

	room, err := h.games.Create(userID, gq)
	if errors.Is(err, game.ErrTooManyRooms) {
		httpx.Error(w, http.StatusServiceUnavailable, "server is busy, try again later")
		return
	}
	if err != nil {
		slog.Error("create room", "err", err)
		httpx.Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]string{"pin": room.PIN()})
}

// GET /api/rooms/{pin} — FE gọi trước khi mở WebSocket để báo lỗi "sai PIN" cho đẹp.
func (h *Handler) roomInfo(w http.ResponseWriter, r *http.Request) {
	room, ok := h.games.Get(r.PathValue("pin"))
	if !ok {
		httpx.Error(w, http.StatusNotFound, "room not found")
		return
	}
	info, ok := room.Info()
	if !ok {
		httpx.Error(w, http.StatusNotFound, "room not found")
		return
	}
	httpx.JSON(w, http.StatusOK, info)
}

// GET /ws/host/{pin}?token=<JWT>
func (h *Handler) hostWS(w http.ResponseWriter, r *http.Request) {
	room, ok := h.games.Get(r.PathValue("pin"))
	if !ok {
		httpx.Error(w, http.StatusNotFound, "room not found")
		return
	}
	// Trình duyệt không cho gắn header Authorization khi mở WebSocket → token đi qua query string.
	userID, err := h.tokens.Verify(r.URL.Query().Get("token"))
	if err != nil {
		httpx.Error(w, http.StatusUnauthorized, "invalid or expired token")
		return
	}
	if userID != room.HostID() {
		httpx.Error(w, http.StatusForbidden, "not the host of this room")
		return
	}
	h.serveWS(w, r, room, game.JoinRequest{IsHost: true, UserID: userID})
}

// GET /ws/play/{pin}?nickname=An  hoặc  ?playerToken=<token> khi kết nối lại
func (h *Handler) playWS(w http.ResponseWriter, r *http.Request) {
	room, ok := h.games.Get(r.PathValue("pin"))
	if !ok {
		httpx.Error(w, http.StatusNotFound, "room not found")
		return
	}
	q := r.URL.Query()
	h.serveWS(w, r, room, game.JoinRequest{Nickname: q.Get("nickname"), Token: q.Get("playerToken")})
}

func (h *Handler) serveWS(w http.ResponseWriter, r *http.Request, room *game.Room, req game.JoinRequest) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: h.wsOrigins})
	if err != nil {
		slog.Warn("websocket accept", "err", err)
		return // Accept đã tự trả HTTP error cho client
	}
	conn.SetReadLimit(4 << 10) // 4KB/tin nhắn là quá đủ

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel() // ReadPump kết thúc → dừng WritePump và watcher

	client := game.NewClient()
	if !room.Join(client, req) {
		conn.Close(websocket.StatusGoingAway, "room closed")
		return
	}

	go client.WritePump(ctx, conn)
	go closeWhenRoomEnds(ctx, conn, room)
	client.ReadPump(ctx, conn, room) // chặn tới khi mất kết nối
}

// closeWhenRoomEnds đảm bảo không có kết nối nào "treo" sau khi phòng đóng.
func closeWhenRoomEnds(ctx context.Context, conn *websocket.Conn, room *game.Room) {
	select {
	case <-ctx.Done():
	case <-room.Done():
		// Cho WritePump 1 giây để gửi close frame lịch sự, sau đó cắt hẳn.
		select {
		case <-ctx.Done():
		case <-time.After(time.Second):
			conn.CloseNow()
		}
	}
}