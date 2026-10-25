package handler

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/khangldit/livequiz-kld/internal/auth"
	"github.com/khangldit/livequiz-kld/internal/db"
	"github.com/khangldit/livequiz-kld/internal/httpx"
)

const defaultTimeLimit = 20

type questionInput struct {
	Text         string   `json:"text"`
	Options      []string `json:"options"`
	CorrectIndex int32    `json:"correctIndex"`
	TimeLimitSec int32    `json:"timeLimitSec"`
}

type createQuizRequest struct {
	Title       string          `json:"title"`
	Description string          `json:"description"`
	Questions   []questionInput `json:"questions"`
}

func (req createQuizRequest) validate() error {
	if strings.TrimSpace(req.Title) == "" {
		return errors.New("title is required")
	}
	if len(req.Questions) < 1 || len(req.Questions) > 50 {
		return errors.New("a quiz needs 1-50 questions")
	}
	for i, q := range req.Questions {
		n := i + 1
		if strings.TrimSpace(q.Text) == "" {
			return fmt.Errorf("question %d: text is required", n)
		}
		if len(q.Options) < 2 || len(q.Options) > 4 {
			return fmt.Errorf("question %d: needs 2-4 options", n)
		}
		if q.CorrectIndex < 0 || int(q.CorrectIndex) >= len(q.Options) {
			return fmt.Errorf("question %d: correctIndex out of range", n)
		}
		if q.TimeLimitSec != 0 && (q.TimeLimitSec < 5 || q.TimeLimitSec > 120) {
			return fmt.Errorf("question %d: timeLimitSec must be 5-120", n)
		}
	}
	return nil
}

type quizDetail struct {
	db.Quiz
	Questions []db.Question `json:"questions"`
}

func (h *Handler) createQuiz(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req createQuizRequest
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := req.validate(); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	// Transaction: quiz + toàn bộ câu hỏi được lưu cùng nhau, hoặc không gì cả.
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		slog.Error("begin tx", "err", err)
		httpx.Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer tx.Rollback(ctx) // không làm gì nếu đã Commit thành công

	qtx := h.q.WithTx(tx)

	quiz, err := qtx.CreateQuiz(ctx, db.CreateQuizParams{
		OwnerID:     auth.UserID(ctx),
		Title:       strings.TrimSpace(req.Title),
		Description: strings.TrimSpace(req.Description),
	})
	if err != nil {
		slog.Error("create quiz", "err", err)
		httpx.Error(w, http.StatusInternalServerError, "internal error")
		return
	}

	questions := make([]db.Question, 0, len(req.Questions))
	for i, q := range req.Questions {
		limit := q.TimeLimitSec
		if limit == 0 {
			limit = defaultTimeLimit
		}
		params := db.CreateQuestionParams{
			QuizID:       quiz.ID,
			Position:     int32(i + 1),
			Text:         strings.TrimSpace(q.Text),
			Options:      q.Options,
			CorrectIndex: q.CorrectIndex,
			TimeLimitSec: limit,
		}
		if err := qtx.CreateQuestion(ctx, params); err != nil {
			slog.Error("create question", "err", err, "position", i+1)
			httpx.Error(w, http.StatusInternalServerError, "internal error")
			return // defer Rollback hủy luôn quiz vừa tạo
		}
		questions = append(questions, db.Question{
			QuizID: quiz.ID, Position: params.Position, Text: params.Text,
			Options: params.Options, CorrectIndex: params.CorrectIndex, TimeLimitSec: limit,
		})
	}

	if err := tx.Commit(ctx); err != nil {
		slog.Error("commit", "err", err)
		httpx.Error(w, http.StatusInternalServerError, "internal error")
		return
	}

	httpx.JSON(w, http.StatusCreated, quizDetail{Quiz: quiz, Questions: questions})
}

func (h *Handler) listQuizzes(w http.ResponseWriter, r *http.Request) {
	items, err := h.q.ListQuizzesByOwner(r.Context(), auth.UserID(r.Context()))
	if err != nil {
		slog.Error("list quizzes", "err", err)
		httpx.Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	if items == nil {
		items = []db.ListQuizzesByOwnerRow{} // trả [] thay vì null cho FE
	}
	httpx.JSON(w, http.StatusOK, items)
}

func (h *Handler) getQuiz(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, ok := parseID(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "invalid quiz id")
		return
	}

	quiz, err := h.q.GetQuiz(ctx, db.GetQuizParams{ID: id, OwnerID: auth.UserID(ctx)})
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
	if questions == nil {
		questions = []db.Question{}
	}
	httpx.JSON(w, http.StatusOK, quizDetail{Quiz: quiz, Questions: questions})
}

func (h *Handler) deleteQuiz(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, ok := parseID(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "invalid quiz id")
		return
	}
	rows, err := h.q.DeleteQuiz(ctx, db.DeleteQuizParams{ID: id, OwnerID: auth.UserID(ctx)})
	if err != nil {
		slog.Error("delete quiz", "err", err)
		httpx.Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	if rows == 0 {
		httpx.Error(w, http.StatusNotFound, "quiz not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func parseID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id, err == nil && id > 0
}