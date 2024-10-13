package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/gorilla/websocket"
)

// WebSocket upgrader
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true }, // Allow all connections
}

// Client represents a single chatting user
type Client struct {
	conn     *websocket.Conn
	room     *Room
	send     chan []byte
	username string
}

// Room represents a chat room
type Room struct {
	name       string
	clients    map[*Client]bool
	broadcast  chan []byte
	register   chan *Client
	unregister chan *Client
}

// Create a new chat room
func newRoom(name string) *Room {
	return &Room{
		name:       name,
		clients:    make(map[*Client]bool),
		broadcast:  make(chan []byte),
		register:   make(chan *Client),
		unregister: make(chan *Client),
	}
}

// Run the room to handle broadcasting and clients joining/leaving
func (r *Room) run() {
	for {
		select {
		case client := <-r.register:
			r.clients[client] = true
		case client := <-r.unregister:
			if _, ok := r.clients[client]; ok {
				delete(r.clients, client)
				close(client.send)
			}
		case message := <-r.broadcast:
			for client := range r.clients {
				select {
				case client.send <- message:
				default:
					close(client.send)
					delete(r.clients, client)
				}
			}
		}
	}
}

// ReadPump handles reading messages from the WebSocket
func (c *Client) readPump() {
	defer func() {
		c.room.unregister <- c
		c.conn.Close()
	}()
	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			log.Println("Read error:", err)
			break
		}
		// Prepend the username to the message
		broadcastMessage := []byte(fmt.Sprintf("%s: %s", c.username, message))
		c.room.broadcast <- broadcastMessage
	}
}

// WritePump handles sending messages to the WebSocket
func (c *Client) writePump() {
	defer c.conn.Close()
	for message := range c.send {
		err := c.conn.WriteMessage(websocket.TextMessage, message)
		if err != nil {
			log.Println("Write error:", err)
			break
		}
	}
}

// WebSocket handler
func serveWs(room *Room, username string, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Upgrade error:", err)
		return
	}
	client := &Client{conn: conn, room: room, send: make(chan []byte, 256), username: username}
	client.room.register <- client

	go client.writePump()
	go client.readPump()
}

// Map to store rooms
var rooms = make(map[string]*Room)

// HTTP handler to join a room
func joinRoom(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "index.html")
	roomName := r.URL.Query().Get("room")
	username := r.URL.Query().Get("username") // Get the username from the query parameters
	if roomName == "" || username == "" {
		http.Error(w, "Room name and username are required", http.StatusBadRequest)
		return
	}

	// If the room doesn't exist, create it
	room, exists := rooms[roomName]
	if !exists {
		room = newRoom(roomName)
		rooms[roomName] = room
		go room.run()
	}

	// Serve the WebSocket connection with the username
	serveWs(room, username, w, r)
}

func main() {
	// fs := http.FileServer(http.Dir("./static")) // Assuming your CSS is in a "static" directory
	// http.Handle("/static/", http.StripPrefix("/static/", fs))
	http.HandleFunc("/", joinRoom)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080" // Fallback port for local testing
	}

	fmt.Println("Server started on port " + port)
	err := http.ListenAndServe(":"+port, nil)
	if err != nil {
		log.Fatal("ListenAndServe error:", err)
	}
}
