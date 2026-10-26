package game

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"sync"
	"time"
)

const maxRooms = 100 // server free chỉ có ít CPU/RAM

var ErrTooManyRooms = errors.New("too many active rooms")

// Manager giữ danh sách phòng đang hoạt động, tra cứu theo mã PIN.
type Manager struct {
	mu    sync.RWMutex
	rooms map[string]*Room

	saver ResultSaver
	saves sync.WaitGroup // các lượt lưu kết quả đang chạy
}

func NewManager(saver ResultSaver) *Manager {
	return &Manager{rooms: make(map[string]*Room), saver: saver}
}

// Create tạo phòng mới với mã PIN 6 số không trùng và khởi chạy goroutine của phòng.
func (m *Manager) Create(hostID int64, quiz Quiz) (*Room, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.rooms) >= maxRooms {
		return nil, ErrTooManyRooms
	}
	for {
		pin, err := randomPIN()
		if err != nil {
			return nil, err
		}
		if _, taken := m.rooms[pin]; taken {
			continue // trùng thì sinh lại (xác suất rất nhỏ)
		}
		room := newRoom(pin, hostID, quiz, m.remove, m.save)
		m.rooms[pin] = room
		go room.run()
		slog.Info("room created", "pin", pin, "quiz", quiz.ID)
		return room, nil
	}
}

func (m *Manager) Get(pin string) (*Room, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	room, ok := m.rooms[pin]
	return room, ok
}

// remove được Room gọi khi goroutine của phòng kết thúc.
func (m *Manager) remove(pin string) {
	m.mu.Lock()
	delete(m.rooms, pin)
	m.mu.Unlock()
	slog.Info("room closed", "pin", pin)
}

// save được Room gọi khi game kết thúc. Chạy ở goroutine riêng để không làm phòng bị khựng.
func (m *Manager) save(res Result) {
	if m.saver == nil {
		return
	}
	m.saves.Add(1)
	go func() {
		defer m.saves.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := m.saver.SaveResult(ctx, res); err != nil {
			slog.Error("save game result", "pin", res.PIN, "err", err)
			return
		}
		slog.Info("game result saved", "pin", res.PIN, "players", len(res.Players))
	}()
}

// Shutdown đóng mọi phòng và chờ các lượt lưu kết quả đang dở (tối đa tới khi ctx hết hạn).
func (m *Manager) Shutdown(ctx context.Context) error {
	m.mu.RLock()
	rooms := make([]*Room, 0, len(m.rooms))
	for _, r := range m.rooms {
		rooms = append(rooms, r)
	}
	m.mu.RUnlock() // nhả lock trước khi Stop: Room sẽ gọi m.remove() cần Lock

	for _, r := range rooms {
		r.Stop()
	}

	done := make(chan struct{})
	go func() {
		m.saves.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func randomPIN() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}