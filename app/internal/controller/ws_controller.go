package controller

import (
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/bujic-movie/bujic-movie/pkg/logger"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

type WSClient struct {
	conn *websocket.Conn
	send chan interface{}
}

type WSController struct {
	clients   map[*WSClient]bool
	clientsMu sync.Mutex
}

var GlobalWSController *WSController

func NewWSController() *WSController {
	GlobalWSController = &WSController{
		clients: make(map[*WSClient]bool),
	}

	logger.LogBroadcaster = func(level string, message string) {
		// Use a goroutine to prevent blocking the logging thread
		go GlobalWSController.Broadcast("log", map[string]string{
			"timestamp": time.Now().Format("2006-01-02 15:04:05"),
			"level":     level,
			"message":   message,
		})
	}

	return GlobalWSController
}

// Handle upgrades the HTTP connection to WebSocket and keeps it alive
func (ctrl *WSController) Handle(c *gin.Context) {
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("Failed to upgrade WebSocket: %v", err)
		return
	}

	client := &WSClient{
		conn: conn,
		send: make(chan interface{}, 256),
	}

	ctrl.clientsMu.Lock()
	ctrl.clients[client] = true
	ctrl.clientsMu.Unlock()

	// Start client write loop
	go client.writeLoop()

	defer func() {
		ctrl.clientsMu.Lock()
		delete(ctrl.clients, client)
		ctrl.clientsMu.Unlock()
		close(client.send)
	}()

	// Keep connection alive until client disconnects
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
	}
}

func (c *WSClient) writeLoop() {
	defer func() {
		c.conn.Close()
	}()

	for {
		msg, ok := <-c.send
		if !ok {
			return
		}

		_ = c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		err := c.conn.WriteJSON(msg)
		if err != nil {
			log.Printf("WebSocket write error: %v", err)
			return
		}
	}
}

type WSMessage struct {
	Type    string      `json:"type"`
	Payload interface{} `json:"payload"`
}

// Broadcast sends JSON payload to all active WebSocket clients non-blockingly
func (ctrl *WSController) Broadcast(msgType string, payload interface{}) {
	ctrl.clientsMu.Lock()
	defer ctrl.clientsMu.Unlock()

	msg := WSMessage{
		Type:    msgType,
		Payload: payload,
	}

	for client := range ctrl.clients {
		select {
		case client.send <- msg:
		default:
			log.Printf("WebSocket client buffer full, dropping message")
		}
	}
}
