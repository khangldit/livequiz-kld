package game

import (
	"log/slog"
	"sync"
	"time"
)

const (
	maxPlayers      = 200
	maxNicknameLen  = 20
	leaderboardSize = 10
	answerGrace     = 300 * time.Millisecond // bù độ trễ mạng cho câu trả lời sát giờ
	idleTimeout     = 30 * time.Minute       // phòng không ai tương tác thì tự đóng
	finishedLinger  = 2 * time.Minute        // giữ phòng thêm 2 phút sau khi game kết thúc
)

type state string

const (
	stateLobby    state = "lobby"
	stateQuestion state = "question"
	stateReveal   state = "reveal"
	stateFinished state = "finished"
)

type eventKind int

const (
	evJoin eventKind = iota
	evLeave
	evMessage
	evInfo
)

type event struct {
	kind   eventKind
	client *Client
	join   JoinRequest
	msg    Message
	reply  chan Info
}

type Player struct {
	ID       string
	token    string // bí mật, dùng để reconnect
	Nickname string
	Score    int
	Correct  int

	client     *Client // nil = đang offline
	answered   bool    // đã trả lời câu hiện tại chưa
	lastAnswer int     // -1 = chưa trả lời
	lastPoints int
}

// Room là 1 phòng game. Toàn bộ state của phòng chỉ được đọc/ghi bởi DUY NHẤT
// goroutine run() → không cần mutex, không có race condition.
// Goroutine khác muốn tác động vào phòng phải gửi event qua channel.
type Room struct {
	pin    string
	hostID int64
	quiz   Quiz

	events   chan event
	quit     chan struct{}
	done     chan struct{}
	stopOnce sync.Once

	onClose  func(pin string)
	onFinish func(Result)

	// ---- Từ đây trở xuống: CHỈ goroutine run() được chạm vào ----
	state     state
	host      *Client
	order     []*Player           // theo thứ tự vào phòng
	byToken   map[string]*Player  // tra cứu khi reconnect
	byClient  map[*Client]*Player // tra cứu khi nhận tin nhắn
	qIndex    int
	qStarted  time.Time
	qTimer    *time.Timer
	answered  int
	startedAt time.Time
	idleAfter time.Duration
}

func newRoom(pin string, hostID int64, quiz Quiz, onClose func(string), onFinish func(Result)) *Room {
	return &Room{
		pin:       pin,
		hostID:    hostID,
		quiz:      quiz,
		events:    make(chan event, 256),
		quit:      make(chan struct{}),
		done:      make(chan struct{}),
		onClose:   onClose,
		onFinish:  onFinish,
		state:     stateLobby,
		byToken:   make(map[string]*Player),
		byClient:  make(map[*Client]*Player),
		qIndex:    -1,
		idleAfter: idleTimeout,
	}
}

// ===================== API an toàn khi gọi từ goroutine khác =====================

func (r *Room) PIN() string           { return r.pin }    // bất biến → đọc thoải mái
func (r *Room) HostID() int64         { return r.hostID } // bất biến
func (r *Room) Done() <-chan struct{} { return r.done }

func (r *Room) Join(c *Client, req JoinRequest) bool {
	return r.enqueue(event{kind: evJoin, client: c, join: req})
}

func (r *Room) Leave(c *Client) {
	r.enqueue(event{kind: evLeave, client: c})
}

func (r *Room) Deliver(c *Client, msg Message) bool {
	return r.enqueue(event{kind: evMessage, client: c, msg: msg})
}

// Info hỏi goroutine của phòng và chờ câu trả lời (pattern request/reply qua channel).
func (r *Room) Info() (Info, bool) {
	reply := make(chan Info, 1)
	if !r.enqueue(event{kind: evInfo, reply: reply}) {
		return Info{}, false
	}
	select {
	case info := <-reply:
		return info, true
	case <-r.done:
		return Info{}, false
	}
}

func (r *Room) Stop() {
	r.stopOnce.Do(func() { close(r.quit) })
}

func (r *Room) enqueue(ev event) bool {
	select {
	case r.events <- ev:
		return true
	case <-r.done:
		return false
	}
}

// ===================== Vòng lặp chính =====================

func (r *Room) run() {
	defer r.cleanup()

	idle := time.NewTimer(r.idleAfter)
	defer idle.Stop()

	for {
		select {
		case ev := <-r.events:
			r.handle(ev)
			if ev.kind != evInfo {
				idle.Reset(r.idleAfter)
			}
		case <-r.timerC():
			r.endQuestion() // hết giờ
		case <-idle.C:
			slog.Info("room idle, closing", "pin", r.pin)
			return
		case <-r.quit:
			return
		}
	}
}

// timerC trả về channel của timer câu hỏi, hoặc nil khi không có câu hỏi đang chạy.
// Nhận từ nil channel sẽ chặn mãi mãi → case đó trong select coi như bị "tắt".
func (r *Room) timerC() <-chan time.Time {
	if r.qTimer == nil {
		return nil
	}
	return r.qTimer.C
}

func (r *Room) cleanup() {
	r.stopQuestionTimer()
	r.closeClient(r.host)
	for _, p := range r.order {
		r.closeClient(p.client)
	}
	close(r.done)
	if r.onClose != nil {
		r.onClose(r.pin)
	}
}

func (r *Room) handle(ev event) {
	switch ev.kind {
	case evJoin:
		r.handleJoin(ev.client, ev.join)
	case evLeave:
		r.handleLeave(ev.client)
	case evMessage:
		r.handleMessage(ev.client, ev.msg)
	case evInfo:
		ev.reply <- Info{PIN: r.pin, QuizTitle: r.quiz.Title, State: string(r.state), PlayerCount: len(r.order)}
	}
}

// ===================== Gửi tin =====================

// sendTo KHÔNG BAO GIỜ chặn: client quá chậm (buffer đầy) bị ngắt kết nối
// thay vì làm cả phòng phải đứng chờ.
func (r *Room) sendTo(c *Client, msg []byte) {
	if c == nil || c.closed {
		return
	}
	select {
	case c.send <- msg:
	default:
		slog.Warn("client too slow, disconnecting", "pin", r.pin)
		r.detach(c)
		r.closeClient(c)
	}
}

func (r *Room) broadcast(msg []byte) {
	r.sendTo(r.host, msg)
	for _, p := range r.order {
		r.sendTo(p.client, msg)
	}
}

func (r *Room) broadcastLobby() {
	msg := encode(TypeLobby, lobbyPayload{Players: r.playerViews()})
	r.sendTo(r.host, msg)
	if r.state == stateLobby { // đang chơi thì chỉ host cần biết ai online/offline
		for _, p := range r.order {
			r.sendTo(p.client, msg)
		}
	}
}

// closeClient đóng channel send đúng 1 lần (đóng 2 lần sẽ panic).
func (r *Room) closeClient(c *Client) {
	if c == nil || c.closed {
		return
	}
	c.closed = true
	close(c.send)
}