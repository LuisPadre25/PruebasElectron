package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	

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
	handleClient func(*Client)
}

func newHub() *Hub {
	return &Hub{
		broadcast:  make(chan []byte),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		clients:    make(map[*Client]bool),
		handleClient: func(client *Client) {
			logServer("info", "👤 Cliente conectado desde %s. Total conectados: %d", 
				client.conn.RemoteAddr(), len(client.hub.clients))
			
			defer func() {
				client.hub.unregister <- client
				client.conn.Close()
				logServer("info", "👋 Cliente desconectado desde %s. Total conectados: %d", 
					client.conn.RemoteAddr(), len(client.hub.clients)-1)
			}()

			for {
				_, message, err := client.conn.ReadMessage()
				if err != nil {
					if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
						logServer("error", "Error leyendo mensaje de %s: %v", 
							client.conn.RemoteAddr(), err)
					}
					break
				}
				logServer("info", "📨 Mensaje recibido de %s [%d bytes]: %s", 
					client.conn.RemoteAddr(), len(message), string(message))
				client.hub.broadcast <- message
			}
		},
	}
}

func (h *Hub) run() {
	for {
		select {
		case client := <-h.register:
			h.clients[client] = true
			logServer("info", "📥 Nuevo cliente registrado desde %s. Total: %d", 
				client.conn.RemoteAddr(), len(h.clients))
		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				
				close(client.send)
				logServer("info", "📤 Cliente desregistrado desde %s. Total: %d", 
					client.conn.RemoteAddr(), len(h.clients))
			}
		case message := <-h.broadcast:
			logServer("info", "📢 Difundiendo mensaje a %d clientes", len(h.clients))
			for client := range h.clients {
				select {
				case client.send <- message:
					logServer("info", "✅ Mensaje enviado a cliente %s", 
						client.conn.RemoteAddr())
				default:
					close(client.send)
					delete(h.clients, client)
					logServer("error", "❌ Error enviando mensaje a %s, cliente eliminado", 
						client.conn.RemoteAddr())
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
				log.Printf("❌ Error leyendo mensaje: %v", err)
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
	logServer("info", "Nueva solicitud WebSocket desde %s", r.RemoteAddr)
	
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		logServer("error", "Error actualizando conexión: %v", err)
		return
	}
	
	client := &Client{hub: hub, conn: conn, send: make(chan []byte, 256)}
	client.hub.register <- client

	logServer("info", "WebSocket establecido con %s", r.RemoteAddr)

	go client.writePump()
	go hub.handleClient(client)
}

type ServerInfo struct {
	IP   string `json:"ip"`
	Port int    `json:"port"`
}

func getServerInfo(w http.ResponseWriter, r *http.Request, port int) {
	logServer("info", "Solicitud de información del servidor desde %s", r.RemoteAddr)
	
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

	logServer("info", "Enviando información del servidor: IP=%s, Puerto=%d", ip, port)
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

func logServer(category string, message string, args ...interface{}) {
	fullMessage := fmt.Sprintf(message, args...)
	
	switch category {
	case "error":
		log.Printf("❌ [Server] %s", fullMessage)
	case "warn":
		log.Printf("⚠️ [Server] %s", fullMessage)
	default:
		log.Printf("🌐 [Server] %s", fullMessage)
	}
}

func main() {
	defaultPort := findServerPort()
	port := flag.Int("port", defaultPort, "Puerto del servidor")
	flag.Parse()

	logServer("info", "Iniciando servidor WebSocket en puerto %d", *port)
	logServer("info", "Este servidor es independiente del nodo P2P que se ejecuta en WASM")

	hub := newHub()
	go hub.run()

	// Verificar que el puerto está disponible antes de iniciar
	addr := fmt.Sprintf(":%d", *port)
	l, err := net.Listen("tcp", addr)
	if err != nil {
		logServer("error", "Puerto %d no está disponible: %v", *port, err)
		os.Exit(1)
	}
	l.Close()

	// Configurar el logger
	log.SetFlags(log.Ltime | log.Ldate)
	logServer("info", "Iniciando servidor en puerto %d", *port)
	logServer("info", "Interfaces de red disponibles:")

	// Listar todas las interfaces de red
	interfaces, err := net.Interfaces()
	if err == nil {
		for _, iface := range interfaces {
			addrs, err := iface.Addrs()
			if err == nil {
				for _, addr := range addrs {
					logServer("info", "  • %s: %s", iface.Name, addr.String())
				}
			}
		}
	}

	// Configurar el logger
	log.SetFlags(log.Ltime | log.Ldate)
	log.Printf("🚀 Iniciando servidor Warcraft LAN...")
	log.Printf("📡 Configurando endpoints...")

	// Configurar las rutas con logging
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("📥 Nueva conexión WebSocket desde: %s", r.RemoteAddr)
		serveWs(hub, w, r)
	})

	http.HandleFunc("/server-info", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("ℹ️ Solicitud de información del servidor desde: %s", r.RemoteAddr)
		getServerInfo(w, r, *port)
	})

	// Servir archivos estáticos desde public
	fs := http.FileServer(http.Dir("public"))
	http.Handle("/", fs)

	// Construir la dirección completa
	address := fmt.Sprintf(":%d", *port)
	
	// Verificar que el puerto esté disponible
	l, err = net.Listen("tcp", address)
	if err != nil {
		log.Fatalf("❌ Error verificando puerto: %v", err)
	}
	l.Close()

	// Banner de inicio
	log.Printf(`
🎮 Servidor Warcraft LAN
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
📍 Puerto: %d
🌐 URL: http://localhost%s
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━`, *port, address)

	log.Printf("✅ Servidor listo y escuchando")
	err = http.ListenAndServe(address, nil)
	if err != nil {
		log.Fatalf("❌ Error fatal: %v", err)
	}
} 