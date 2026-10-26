package game

import (
	"context"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

const (
	writeTimeout = 5 * time.Second
	pingInterval = 25 * time.Second
)

// Mỗi kết nối WebSocket có đúng 2 goroutine:
//   - WritePump: goroutine DUY NHẤT ghi vào conn.
//   - ReadPump:  goroutine DUY NHẤT đọc từ conn.

// WritePump lấy tin nhắn từ c.send và ghi ra WebSocket; định kỳ ping để giữ kết nối.
func (c *Client) WritePump(ctx context.Context, conn *websocket.Conn) {
	defer conn.CloseNow() // lỗi ghi → cắt kết nối để ReadPump cũng dừng

	ping := time.NewTicker(pingInterval)
	defer ping.Stop()

	for {
		select {
		case msg, ok := <-c.send:
			if !ok { // Room đã đóng client này → đóng kết nối lịch sự
				conn.Close(websocket.StatusNormalClosure, "")
				return
			}
			wctx, cancel := context.WithTimeout(ctx, writeTimeout)
			err := conn.Write(wctx, websocket.MessageText, msg)
			cancel()
			if err != nil {
				return
			}
		case <-ping.C:
			pctx, cancel := context.WithTimeout(ctx, writeTimeout)
			err := conn.Ping(pctx)
			cancel()
			if err != nil {
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

// ReadPump đọc tin nhắn từ WebSocket và chuyển cho Room. Chạy đến khi kết nối đứt.
func (c *Client) ReadPump(ctx context.Context, conn *websocket.Conn, room *Room) {
	defer room.Leave(c)
	for {
		var msg Message
		if err := wsjson.Read(ctx, conn, &msg); err != nil {
			return
		}
		if !room.Deliver(c, msg) {
			return // phòng đã đóng
		}
	}
}