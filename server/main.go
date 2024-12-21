package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	

	"github.com/gorilla/websocket"
)

var addr = flag.String("addr", ":8080", "Dirección del servidor")

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Permitir todas las conexiones en desarrollo
	},
}

type Client struct {
	hub  *Hub
	conn *websocket.Conn
	send chan []byte
}

type Hub struct {
	clients    map[*Client]bool
	broadcast  chan []byte
	register   chan *Client
	unregister chan *Client
}

func newHub() *Hub {
	return &Hub{
		broadcast:  make(chan []byte),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		clients:    make(map[*Client]bool),
	}
}

func (h *Hub) run() {
	for {
		select {
		case client := <-h.register:
			h.clients[client] = true
			log.Printf("Nuevo cliente conectado. Total: %d", len(h.clients))
		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
				log.Printf("Cliente desconectado. Total: %d", len(h.clients))
			}
		case message := <-h.broadcast:
			for client := range h.clients {
				select {
				case client.send <- message:
				default:
					close(client.send)
					delete(h.clients, client)
				}
			}
		}
	}
}

func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("error: %v", err)
			}
			break
		}
		c.hub.broadcast <- message
	}
}

func (c *Client) writePump() {
	defer func() {
		c.conn.Close()
	}()

	for {
		message, ok := <-c.send
		if !ok {
			c.conn.WriteMessage(websocket.CloseMessage, []byte{})
			return
		}

		err := c.conn.WriteMessage(websocket.TextMessage, message)
		if err != nil {
			return
		}
	}
}

func serveWs(hub *Hub, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}
	client := &Client{hub: hub, conn: conn, send: make(chan []byte, 256)}
	client.hub.register <- client

	go client.writePump()
	go client.readPump()
}

type ServerInfo struct {
	IP   string `json:"ip"`
	Port int    `json:"port"`
}

func getServerInfo(w http.ResponseWriter, r *http.Request, port int) {
	// Configurar CORS
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Content-Type", "application/json")

	// Si es una solicitud OPTIONS, retornar inmediatamente
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	addrs, err := net.InterfaceAddrs()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var ip string
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				ip = ipnet.IP.String()
				break
			}
		}
	}

	info := ServerInfo{
		IP:   ip,
		Port: port,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(info)
}

func findServerPort() int {
	// Rangos de puertos para el servidor web
	// Evitamos puertos comunes
	startPort := 9000
	endPort := 9099

	for port := startPort; port <= endPort; port++ {
		listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
		if err == nil {
			listener.Close()
			return port
		}
	}
	return 8080 // Puerto por defecto si no se encuentra uno disponible
}

func main() {
	// Encontrar un puerto disponible
	defaultPort := findServerPort()
	
	// Usar directamente flag.Int en lugar de una variable global
	port := flag.Int("port", defaultPort, "Puerto del servidor")
	flag.Parse()

	hub := newHub()
	go hub.run()

	// Configurar las rutas
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("Nueva conexión WebSocket desde: %s", r.RemoteAddr)
		serveWs(hub, w, r)
	})

	http.HandleFunc("/server-info", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("Solicitud de información del servidor desde: %s", r.RemoteAddr)
		getServerInfo(w, r, *port)
	})

	// Servir archivos estáticos desde public
	fs := http.FileServer(http.Dir("public"))
	http.Handle("/", fs)

	// Construir la dirección completa
	address := fmt.Sprintf(":%d", *port)
	log.Printf("Iniciando servidor en %s...", address)

	// Verificar que el puerto esté disponible
	listener, err := net.Listen("tcp", address)
	if err != nil {
		log.Fatalf("Error verificando puerto: %v", err)
	}
	listener.Close()

	log.Printf("Servidor listo en http://localhost%s", address)
	err = http.ListenAndServe(address, nil)
	if err != nil {
		log.Fatal("Error iniciando servidor:", err)
	}
} 