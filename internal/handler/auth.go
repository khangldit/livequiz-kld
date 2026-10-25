package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/crypto/bcrypt"

	"github.com/khangldit/livequiz-kld/internal/auth"
	"github.com/khangldit/livequiz-kld/internal/db"
	"github.com/khangldit/livequiz-kld/internal/httpx"
)

const tokenTTL = 7 * 24 * time.Hour

type registerRequest struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"displayName"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type userResponse struct {
	ID          int64  `json:"id"`
	Email       string `json:"email"`
	DisplayName string `json:"displayName"`
}

type authResponse struct {
	Token string       `json:"token"`
	User  userResponse `json:"user"`
}

func (h *Handler) register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	req.DisplayName = strings.TrimSpace(req.DisplayName)

	if !strings.Contains(req.Email, "@") || req.DisplayName == "" {
		httpx.Error(w, http.StatusBadRequest, "valid email and displayName are required")
		return
	}
	if len(req.Password) < 8 || len(req.Password) > 72 {
		httpx.Error(w, http.StatusBadRequest, "password must be 8-72 characters")
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		slog.Error("hash password", "err", err)
		httpx.Error(w, http.StatusInternalServerError, "internal error")
		return
	}

	user, err := h.q.CreateUser(r.Context(), db.CreateUserParams{
		Email:        req.Email,
		PasswordHash: string(hash),
		DisplayName:  req.DisplayName,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique_violation
			httpx.Error(w, http.StatusConflict, "email already registered")
			return
		}
		slog.Error("create user", "err", err)
		httpx.Error(w, http.StatusInternalServerError, "internal error")
		return
	}

	h.respondWithToken(w, http.StatusCreated, userResponse{
		ID: user.ID, Email: user.Email, DisplayName: user.DisplayName,
	})
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	user, err := h.q.GetUserByEmail(r.Context(), strings.ToLower(strings.TrimSpace(req.Email)))
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.Error(w, http.StatusUnauthorized, "invalid email or password")
		return
	}
	if err != nil {
		slog.Error("get user", "err", err)
		httpx.Error(w, http.StatusInternalServerError, "internal error")
		return
	}

	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)) != nil {
		httpx.Error(w, http.StatusUnauthorized, "invalid email or password")
		return
	}

	h.respondWithToken(w, http.StatusOK, userResponse{
		ID: user.ID, Email: user.Email, DisplayName: user.DisplayName,
	})
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	user, err := h.q.GetUserByID(r.Context(), auth.UserID(r.Context()))
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "user not found")
		return
	}
	httpx.JSON(w, http.StatusOK, userResponse{
		ID: user.ID, Email: user.Email, DisplayName: user.DisplayName,
	})
}

func (h *Handler) respondWithToken(w http.ResponseWriter, status int, u userResponse) {
	token, err := h.tokens.Create(u.ID, tokenTTL)
	if err != nil {
		slog.Error("create token", "err", err)
		httpx.Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	httpx.JSON(w, status, authResponse{Token: token, User: u})
}