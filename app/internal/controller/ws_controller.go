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

type WSController struct {
	clients   map[*websocket.Conn]bool
	clientsMu sync.Mutex
}

var GlobalWSController *WSController

func NewWSController() *WSController {
	GlobalWSController = &WSController{
		clients: make(map[*websocket.Conn]bool),
	}

	logger.LogBroadcaster = func(level string, message string) {
		GlobalWSController.Broadcast("log", map[string]string{
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

	ctrl.clientsMu.Lock()
	ctrl.clients[conn] = true
	ctrl.clientsMu.Unlock()

	defer func() {
		ctrl.clientsMu.Lock()
		delete(ctrl.clients, conn)
		ctrl.clientsMu.Unlock()
		conn.Close()
	}()

	// Keep connection alive until client disconnects
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
	}
}

type WSMessage struct {
	Type    string      `json:"type"`
	Payload interface{} `json:"payload"`
}

// Broadcast sends JSON payload to all active WebSocket clients
func (ctrl *WSController) Broadcast(msgType string, payload interface{}) {
	ctrl.clientsMu.Lock()
	defer ctrl.clientsMu.Unlock()

	msg := WSMessage{
		Type:    msgType,
		Payload: payload,
	}

	for client := range ctrl.clients {
		err := client.WriteJSON(msg)
		if err != nil {
			log.Printf("WebSocket write error, closing client: %v", err)
			client.Close()
			delete(ctrl.clients, client)
		}
	}
}
