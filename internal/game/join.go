package game

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"unicode/utf8"
)

// Xử lý người vào / ra phòng. Mọi hàm ở đây chỉ chạy trong goroutine run() của Room.

func (r *Room) handleJoin(c *Client, req JoinRequest) {
	switch {
	case req.IsHost:
		r.joinHost(c, req.UserID)
	case req.Token != "":
		r.rejoinPlayer(c, req.Token)
	default:
		r.joinNewPlayer(c, req.Nickname)
	}
}

func (r *Room) joinHost(c *Client, userID int64) {
	if userID != r.hostID {
		r.reject(c, "not the host of this room")
		return
	}
	old := r.host
	r.host = c
	r.closeClient(old) // host mở tab mới → đóng tab cũ
	r.sendTo(c, encode(TypeState, r.snapshot(nil)))
}

func (r *Room) joinNewPlayer(c *Client, nickname string) {
	nickname = strings.TrimSpace(nickname)
	switch {
	case r.state != stateLobby:
		r.reject(c, "game already started")
		return
	case nickname == "" || utf8.RuneCountInString(nickname) > maxNicknameLen:
		r.reject(c, "nickname must be 1-20 characters")
		return
	case len(r.order) >= maxPlayers:
		r.reject(c, "room is full")
		return
	case r.nicknameTaken(nickname):
		r.reject(c, "nickname already taken")
		return
	}

	p := &Player{ID: randomHex(4), token: randomHex(16), Nickname: nickname, lastAnswer: -1}
	r.order = append(r.order, p)
	r.byToken[p.token] = p
	r.attach(p, c)

	r.sendTo(c, encode(TypeWelcome, welcomePayload{PlayerID: p.ID, PlayerToken: p.token, Nickname: p.Nickname}))
	r.sendTo(c, encode(TypeState, r.snapshot(p)))
	r.broadcastLobby()
}

func (r *Room) rejoinPlayer(c *Client, token string) {
	p, ok := r.byToken[token]
	if !ok {
		r.reject(c, "session not found, please join again")
		return
	}
	r.attach(p, c)
	r.sendTo(c, encode(TypeWelcome, welcomePayload{PlayerID: p.ID, PlayerToken: p.token, Nickname: p.Nickname}))
	r.sendTo(c, encode(TypeState, r.snapshot(p)))
	r.broadcastLobby()
}

func (r *Room) handleLeave(c *Client) {
	_, wasPlayer := r.byClient[c]
	r.detach(c)
	r.closeClient(c) // an toàn kể cả khi c đã bị đóng trước đó
	if wasPlayer {
		r.broadcastLobby()
		r.maybeEndEarly() // người vừa thoát có thể là người cuối chưa trả lời
	}
}

// attach gắn kết nối mới cho player; nếu player đang có kết nối cũ (tab cũ) thì đóng nó.
func (r *Room) attach(p *Player, c *Client) {
	if old := p.client; old != nil {
		r.detach(old)
		r.closeClient(old)
	}
	p.client = c
	r.byClient[c] = p
}

// detach gỡ client khỏi phòng (host hoặc player) nhưng GIỮ player và điểm số.
func (r *Room) detach(c *Client) {
	if c == nil {
		return
	}
	if c == r.host {
		r.host = nil
	}
	if p, ok := r.byClient[c]; ok {
		p.client = nil
		delete(r.byClient, c)
	}
}

func (r *Room) reject(c *Client, reason string) {
	r.sendTo(c, errorMsg(reason))
	r.closeClient(c)
}

func (r *Room) nicknameTaken(nickname string) bool {
	for _, p := range r.order {
		if strings.EqualFold(p.Nickname, nickname) {
			return true
		}
	}
	return false
}

func randomHex(nBytes int) string {
	b := make([]byte, nBytes)
	_, _ = rand.Read(b) // crypto/rand: an toàn để làm token bí mật
	return hex.EncodeToString(b)
}