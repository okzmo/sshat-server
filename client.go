package main

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/fasthttp/websocket"
)

type Client struct {
	hub  *Hub
	conn *websocket.Conn
	send chan Message
}

type Message struct {
	RoleName    string `json:"role"`
	RoleColor   string `json:"roleColor"`
	SenderName  string `json:"sender"`
	SenderColor string `json:"userColor"`
	Content     []byte `json:"content"`
}

const (
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	writeWait      = 10 * time.Second
	maxMessageSize = 512
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

func (c *Client) readMessage() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()
	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})
	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseAbnormalClosure, websocket.CloseGoingAway) {
				log.Printf("ERR: %v", err)
			}
			break
		}

		var chatMessage Message
		err = json.Unmarshal(message, &chatMessage)
		if err != nil {
			log.Printf("ERROR ON READ: %v", err)
			return
		}

		c.hub.broadcast <- Message{
			RoleName:    chatMessage.RoleName,
			RoleColor:   chatMessage.RoleColor,
			SenderName:  chatMessage.SenderName,
			SenderColor: chatMessage.SenderColor,
			Content:     chatMessage.Content,
		}
	}
}

func (c *Client) WriteMessage() {
	ticker := time.NewTicker(writeWait)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()
	for {
		select {
		case message, ok := <-c.send:
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}

			msgByte, err := json.Marshal(message)
			if err != nil {
				log.Printf("ERROR ON WRITE: %v", err)
				return
			}

			w.Write(msgByte)

			// n := len(c.send)
			// for i := 0; i < n; i++ {
			// 	w.Write([]byte{'\n'})
			// 	w.Write(<-c.send)
			// }

			if err := w.Close(); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func upgradeWS(hub *Hub, w http.ResponseWriter, r *http.Request) {
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ERR: %v", err)
		return
	}

	client := &Client{hub: hub, conn: c, send: make(chan Message)}
	client.hub.register <- client

	go client.readMessage()
	go client.WriteMessage()
}
