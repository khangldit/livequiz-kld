package game

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"
)

// Luồng chơi: lệnh của host, nhận đáp án, chấm điểm, công bố kết quả.
// Mọi hàm ở đây chỉ chạy trong goroutine run() của Room.

// ===================== Tin nhắn =====================

func (r *Room) handleMessage(c *Client, msg Message) {
	if c == r.host {
		r.handleHostCommand(msg.Type)
		return
	}
	p, ok := r.byClient[c]
	if !ok || msg.Type != TypeAnswer {
		return
	}
	var a answerPayload
	if err := json.Unmarshal(msg.Payload, &a); err != nil {
		r.sendTo(c, errorMsg("invalid answer payload"))
		return
	}
	r.handleAnswer(p, a)
}

func (r *Room) handleHostCommand(typ string) {
	switch {
	case typ == TypeStart && r.state == stateLobby:
		if len(r.order) == 0 {
			r.sendTo(r.host, errorMsg("need at least 1 player to start"))
			return
		}
		r.startedAt = time.Now()
		r.startQuestion(0)
	case typ == TypeSkip && r.state == stateQuestion:
		r.endQuestion()
	case typ == TypeNext && r.state == stateReveal:
		if r.qIndex+1 < len(r.quiz.Questions) {
			r.startQuestion(r.qIndex + 1)
		} else {
			r.finish()
		}
	default:
		r.sendTo(r.host, errorMsg(fmt.Sprintf("cannot %q while room is %q", typ, r.state)))
	}
}

// ===================== Game loop =====================

func (r *Room) startQuestion(i int) {
	q := r.quiz.Questions[i]
	r.state = stateQuestion
	r.qIndex = i
	r.qStarted = time.Now()
	r.answered = 0
	for _, p := range r.order {
		p.answered = false
		p.lastAnswer = -1
		p.lastPoints = 0
	}
	r.qTimer = time.NewTimer(q.TimeLimit + answerGrace)
	r.broadcast(encode(TypeQuestion, r.questionPayload()))
}

func (r *Room) handleAnswer(p *Player, a answerPayload) {
	if r.state != stateQuestion || a.QuestionIndex != r.qIndex || p.answered {
		return // sai câu, hết giờ, hoặc đã trả lời rồi → bỏ qua
	}
	q := r.quiz.Questions[r.qIndex]
	if a.OptionIndex < 0 || a.OptionIndex >= len(q.Options) {
		return
	}

	// Server tự đo thời gian — không tin bất kỳ timestamp nào từ client.
	elapsed := time.Since(r.qStarted)
	correct := a.OptionIndex == q.CorrectIndex
	points := Score(correct, elapsed, q.TimeLimit)

	p.answered = true
	p.lastAnswer = a.OptionIndex
	p.lastPoints = points
	p.Score += points
	if correct {
		p.Correct++
	}
	r.answered++

	r.sendTo(p.client, encode(TypeAnswerAck, map[string]int{"optionIndex": a.OptionIndex}))
	r.sendTo(r.host, encode(TypeAnswerCount, answerCountPayload{Answered: r.answered, Total: len(r.order)}))
	r.maybeEndEarly()
}

// maybeEndEarly kết thúc câu hỏi ngay khi mọi người đang online đã trả lời.
func (r *Room) maybeEndEarly() {
	if r.state != stateQuestion {
		return
	}
	for _, p := range r.order {
		if p.client != nil && !p.answered {
			return
		}
	}
	r.endQuestion()
}

func (r *Room) endQuestion() {
	r.stopQuestionTimer()
	r.state = stateReveal

	q := r.quiz.Questions[r.qIndex]
	dist := make([]int, len(q.Options))
	for _, p := range r.order {
		if p.lastAnswer >= 0 {
			dist[p.lastAnswer]++
		}
	}
	ranked := r.ranking()

	r.sendTo(r.host, encode(TypeReveal, hostRevealPayload{
		QuestionIndex: r.qIndex,
		CorrectIndex:  q.CorrectIndex,
		Distribution:  dist,
		Leaderboard:   leaderboard(ranked, leaderboardSize),
		IsLast:        r.qIndex == len(r.quiz.Questions)-1,
	}))
	for i, p := range ranked {
		r.sendTo(p.client, encode(TypeReveal, playerRevealPayload{
			QuestionIndex: r.qIndex,
			CorrectIndex:  q.CorrectIndex,
			YourAnswer:    p.lastAnswer,
			Correct:       p.lastAnswer == q.CorrectIndex,
			Points:        p.lastPoints,
			Score:         p.Score,
			Rank:          i + 1,
		}))
	}
}

func (r *Room) finish() {
	r.state = stateFinished
	r.idleAfter = finishedLinger

	ranked := r.ranking()
	board := leaderboard(ranked, leaderboardSize)
	r.sendTo(r.host, encode(TypeGameOver, gameOverPayload{Leaderboard: board}))
	for i, p := range ranked {
		me := rankEntry{Rank: i + 1, Nickname: p.Nickname, Score: p.Score}
		r.sendTo(p.client, encode(TypeGameOver, gameOverPayload{Leaderboard: board, Me: &me}))
	}

	if r.onFinish != nil {
		r.onFinish(r.result(ranked))
	}
}

func (r *Room) stopQuestionTimer() {
	if r.qTimer != nil {
		r.qTimer.Stop()
		r.qTimer = nil
	}
}

// ===================== Helpers =====================

func (r *Room) questionPayload() *questionPayload {
	q := r.quiz.Questions[r.qIndex]
	return &questionPayload{
		Index:        r.qIndex,
		Total:        len(r.quiz.Questions),
		Text:         q.Text,
		Options:      q.Options,
		TimeLimitSec: int(q.TimeLimit / time.Second),
		EndsAt:       r.qStarted.Add(q.TimeLimit).UnixMilli(),
	}
}

func (r *Room) snapshot(me *Player) statePayload {
	s := statePayload{
		State:          string(r.state),
		PIN:            r.pin,
		QuizTitle:      r.quiz.Title,
		TotalQuestions: len(r.quiz.Questions),
		Players:        r.playerViews(),
	}
	if r.state == stateQuestion {
		s.Question = r.questionPayload()
	}
	if me != nil {
		s.Me = &meView{Nickname: me.Nickname, Score: me.Score, Answered: me.answered}
	}
	return s
}

func (r *Room) playerViews() []playerView {
	views := make([]playerView, len(r.order))
	for i, p := range r.order {
		views[i] = playerView{ID: p.ID, Nickname: p.Nickname, Online: p.client != nil}
	}
	return views
}

// ranking: điểm giảm dần; bằng điểm thì ai vào phòng trước xếp trước (sort ổn định).
func (r *Room) ranking() []*Player {
	ranked := slices.Clone(r.order)
	slices.SortStableFunc(ranked, func(a, b *Player) int { return b.Score - a.Score })
	return ranked
}

func leaderboard(ranked []*Player, n int) []rankEntry {
	n = min(n, len(ranked))
	board := make([]rankEntry, n)
	for i := range n {
		board[i] = rankEntry{Rank: i + 1, Nickname: ranked[i].Nickname, Score: ranked[i].Score}
	}
	return board
}

func (r *Room) result(ranked []*Player) Result {
	res := Result{
		QuizID:     r.quiz.ID,
		HostID:     r.hostID,
		QuizTitle:  r.quiz.Title,
		PIN:        r.pin,
		StartedAt:  r.startedAt,
		FinishedAt: time.Now(),
	}
	for i, p := range ranked {
		res.Players = append(res.Players, PlayerResult{Nickname: p.Nickname, Score: p.Score, Rank: i + 1, Correct: p.Correct})
	}
	return res
}