package game

const sendBuffer = 32

// Client đại diện cho 1 kết nối (host hoặc player) từ góc nhìn của Room.
//
// Quy tắc sở hữu (rất quan trọng):
//   - Chỉ goroutine của Room được gửi vào c.send, đóng c.send và đọc/ghi c.closed.
//   - Room KHÔNG biết gì về WebSocket → test được Room mà không cần mạng.
type Client struct {
	send   chan []byte
	closed bool
}

func NewClient() *Client {
	return &Client{send: make(chan []byte, sendBuffer)}
}