package signaling

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Handler func(msg map[string]any)

type Signaling struct {
	url     string
	topic   string
	writeCh chan []byte

	handMu   sync.RWMutex
	handlers []Handler

	done chan struct{}
	once sync.Once
}

func New(url, topic string) *Signaling {
	s := &Signaling{
		url:     url,
		topic:   topic,
		writeCh: make(chan []byte, 64),
		done:    make(chan struct{}),
	}
	go s.run()
	return s
}

func (s *Signaling) run() {
	for {
		if err := s.connect(); err != nil {
			select {
			case <-s.done:
				return
			case <-time.After(2 * time.Second):
			}
		} else {
			return
		}
	}
}

func (s *Signaling) connect() error {
	conn, _, err := websocket.DefaultDialer.Dial(s.url, nil)
	if err != nil {
		return err
	}

	// Subscribe immediately (synchronous — must be first message)
	if err := conn.WriteJSON(map[string]any{
		"type":   "subscribe",
		"topics": []string{s.topic},
	}); err != nil {
		conn.Close()
		return err
	}

	writeErrCh := make(chan error, 1)

	// Write goroutine — gorilla requires serialised writes
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case data := <-s.writeCh:
				if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
					writeErrCh <- err
					return
				}
			case <-ticker.C:
				if err := conn.WriteJSON(map[string]any{"type": "ping"}); err != nil {
					writeErrCh <- err
					return
				}
			case <-s.done:
				conn.WriteJSON(map[string]any{ //nolint:errcheck
					"type":   "unsubscribe",
					"topics": []string{s.topic},
				})
				conn.Close()
				return
			}
		}
	}()

	// Read loop
	for {
		select {
		case err := <-writeErrCh:
			conn.Close()
			return err
		default:
		}

		_, data, err := conn.ReadMessage()
		if err != nil {
			conn.Close()
			return err
		}

		var msg map[string]any
		if json.Unmarshal(data, &msg) != nil {
			continue
		}
		if msg["type"] != "publish" || msg["topic"] != s.topic {
			continue
		}
		payload, ok := msg["data"].(map[string]any)
		if !ok {
			continue
		}

		s.handMu.RLock()
		handlers := make([]Handler, len(s.handlers))
		copy(handlers, s.handlers)
		s.handMu.RUnlock()

		for _, h := range handlers {
			go h(payload)
		}
	}
}

// Publish sends a message to all other subscribers on the topic.
func (s *Signaling) Publish(data map[string]any) {
	b, err := json.Marshal(map[string]any{
		"type":  "publish",
		"topic": s.topic,
		"data":  data,
	})
	if err != nil {
		return
	}
	select {
	case s.writeCh <- b:
	case <-s.done:
	}
}

// OnMessage registers a handler for incoming published messages.
func (s *Signaling) OnMessage(h Handler) {
	s.handMu.Lock()
	s.handlers = append(s.handlers, h)
	s.handMu.Unlock()
}

func (s *Signaling) Close() {
	s.once.Do(func() { close(s.done) })
}
