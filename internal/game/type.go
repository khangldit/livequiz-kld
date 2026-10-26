package game

import (
	"context"
	"time"
)

// Quiz là bản sao dữ liệu quiz mà phòng game giữ trong RAM.
// Package game không import package db: logic game không phụ thuộc vào cách lưu trữ.
type Quiz struct {
	ID        int64
	Title     string
	Questions []Question
}

type Question struct {
	Text         string
	Options      []string
	CorrectIndex int
	TimeLimit    time.Duration
}

// JoinRequest mô tả ai đang kết nối vào phòng.
type JoinRequest struct {
	IsHost   bool
	UserID   int64  // host: lấy từ JWT
	Nickname string // player mới
	Token    string // player kết nối lại
}

// Info là thông tin công khai của phòng (cho màn hình nhập PIN).
type Info struct {
	PIN         string `json:"pin"`
	QuizTitle   string `json:"quizTitle"`
	State       string `json:"state"`
	PlayerCount int    `json:"playerCount"`
}

// Result là kết quả cuối game, được lưu xuống DB ở Phần 3.
type Result struct {
	QuizID     int64
	HostID     int64
	QuizTitle  string
	PIN        string
	StartedAt  time.Time
	FinishedAt time.Time
	Players    []PlayerResult
}

type PlayerResult struct {
	Nickname string
	Score    int
	Rank     int
	Correct  int
}

// ResultSaver được Manager gọi khi game kết thúc. nil = không lưu.
type ResultSaver interface {
	SaveResult(ctx context.Context, res Result) error
}