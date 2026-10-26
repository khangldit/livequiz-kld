package game

import "encoding/json"

// Message là "phong bì" chung cho mọi tin nhắn WebSocket:
// {"type": "answer", "payload": {...}}
type Message struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// Client → Server
const (
	TypeStart  = "start"  // host: bắt đầu game
	TypeSkip   = "skip"   // host: kết thúc câu hiện tại sớm
	TypeNext   = "next"   // host: sang câu tiếp theo (hoặc kết thúc game ở câu cuối)
	TypeAnswer = "answer" // player: gửi đáp án
)

// Server → Client
const (
	TypeError       = "error"
	TypeWelcome     = "welcome"      // player: định danh + token để reconnect
	TypeState       = "state"        // snapshot trạng thái khi (re)connect
	TypeLobby       = "lobby"        // danh sách người chơi
	TypeQuestion    = "question"     // câu hỏi mới — KHÔNG kèm đáp án đúng
	TypeAnswerAck   = "answer_ack"   // player: server đã nhận đáp án
	TypeAnswerCount = "answer_count" // host: số người đã trả lời
	TypeReveal      = "reveal"       // công bố kết quả câu hỏi
	TypeGameOver    = "game_over"    // bảng xếp hạng cuối cùng
)

type answerPayload struct {
	QuestionIndex int `json:"questionIndex"`
	OptionIndex   int `json:"optionIndex"`
}

type playerView struct {
	ID       string `json:"id"`
	Nickname string `json:"nickname"`
	Online   bool   `json:"online"`
}

type lobbyPayload struct {
	Players []playerView `json:"players"`
}

type welcomePayload struct {
	PlayerID    string `json:"playerId"`
	PlayerToken string `json:"playerToken"`
	Nickname    string `json:"nickname"`
}

type questionPayload struct {
	Index        int      `json:"index"`
	Total        int      `json:"total"`
	Text         string   `json:"text"`
	Options      []string `json:"options"`
	TimeLimitSec int      `json:"timeLimitSec"`
	EndsAt       int64    `json:"endsAt"` // unix milliseconds — FE dùng để đếm ngược
}

type answerCountPayload struct {
	Answered int `json:"answered"`
	Total    int `json:"total"`
}

type rankEntry struct {
	Rank     int    `json:"rank"`
	Nickname string `json:"nickname"`
	Score    int    `json:"score"`
}

type hostRevealPayload struct {
	QuestionIndex int         `json:"questionIndex"`
	CorrectIndex  int         `json:"correctIndex"`
	Distribution  []int       `json:"distribution"` // số người chọn mỗi đáp án
	Leaderboard   []rankEntry `json:"leaderboard"`
	IsLast        bool        `json:"isLast"`
}

type playerRevealPayload struct {
	QuestionIndex int  `json:"questionIndex"`
	CorrectIndex  int  `json:"correctIndex"`
	YourAnswer    int  `json:"yourAnswer"` // -1 = không trả lời
	Correct       bool `json:"correct"`
	Points        int  `json:"points"`
	Score         int  `json:"score"`
	Rank          int  `json:"rank"`
}

type gameOverPayload struct {
	Leaderboard []rankEntry `json:"leaderboard"`
	Me          *rankEntry  `json:"me,omitempty"` // chỉ gửi cho player
}

type meView struct {
	Nickname string `json:"nickname"`
	Score    int    `json:"score"`
	Answered bool   `json:"answered"`
}

type statePayload struct {
	State          string           `json:"state"`
	PIN            string           `json:"pin"`
	QuizTitle      string           `json:"quizTitle"`
	TotalQuestions int              `json:"totalQuestions"`
	Players        []playerView     `json:"players"`
	Question       *questionPayload `json:"question,omitempty"`
	Me             *meView          `json:"me,omitempty"`
}

// encode đóng gói type + payload thành JSON. Gọi 1 lần rồi gửi cùng []byte cho nhiều client.
func encode(typ string, payload any) []byte {
	b, err := json.Marshal(struct {
		Type    string `json:"type"`
		Payload any    `json:"payload,omitempty"`
	}{typ, payload})
	if err != nil {
		panic(err) // chỉ xảy ra khi payload có kiểu không encode được: lỗi lập trình
	}
	return b
}

func errorMsg(text string) []byte {
	return encode(TypeError, map[string]string{"message": text})
}