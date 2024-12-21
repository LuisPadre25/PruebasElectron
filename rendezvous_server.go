package main

import (
    "encoding/json"
    "fmt"
    "net"
    "os"
    "sync"
)

type ClientInfo struct {
    ID        string
    PublicIP  string
    PublicPort int
    LocalIP   string
    LocalPort int
}

type RendezvousServer struct {
    clients     map[string]*ClientInfo
    clientsMux  sync.RWMutex
    listener    net.Listener
}

func NewRendezvousServer() *RendezvousServer {
    return &RendezvousServer{
        clients: make(map[string]*ClientInfo),
    }
}

func (s *RendezvousServer) Start() error {
    // Mostrar IPs disponibles
    fmt.Println("\n=== Servidor Rendezvous P2P ===")
    fmt.Println("IPs disponibles:")
    ifaces, err := net.Interfaces()
    if err == nil {
        for _, iface := range ifaces {
            addrs, err := iface.Addrs()
            if err == nil {
                for _, addr := range addrs {
                    if ipnet, ok := addr.(*net.IPNet); ok {
                        if ipnet.IP.To4() != nil {
                            fmt.Printf("- Interface %v: %v\n", iface.Name, ipnet.IP.String())
                        }
                    }
                }
            }
        }
    }

    // Obtener IP principal
    localIP := getLocalIP()
    fmt.Printf("\nIP principal del servidor rendezvous: %s\n", localIP)
    fmt.Printf("Puerto: %s\n", DEFAULT_PORT)
    fmt.Printf("Los clientes deben conectarse usando: %s:%s\n", localIP, DEFAULT_PORT)

    // Iniciar el servidor
    fmt.Println("\nIniciando servidor rendezvous...")
    listener, err := net.Listen("tcp", ":"+DEFAULT_PORT)
    if err != nil {
        return fmt.Errorf("error al iniciar el servidor: %v", err)
    }
    s.listener = listener

    fmt.Printf("\nServidor rendezvous escuchando en %s\n", s.listener.Addr().String())
    fmt.Println("Presione Ctrl+C para cerrar el servidor")
    fmt.Println("----------------------------------------")

    // Aceptar conexiones
    for {
        conn, err := listener.Accept()
        if err != nil {
            fmt.Printf("Error aceptando conexión: %v\n", err)
            continue
        }
        go s.handleConnection(conn)
    }
}

func (s *RendezvousServer) handleConnection(conn net.Conn) {
    defer conn.Close()
    
    fmt.Printf("\n[+] Nueva conexión desde %s\n", conn.RemoteAddr())
    
    // Obtener información del cliente
    remoteAddr := conn.RemoteAddr().(*net.TCPAddr)
    clientInfo := &ClientInfo{
        PublicIP:   remoteAddr.IP.String(),
        PublicPort: remoteAddr.Port,
    }
    
    // Recibir información local del cliente
    decoder := json.NewDecoder(conn)
    if err := decoder.Decode(clientInfo); err != nil {
        fmt.Printf("[-] Error decodificando info del cliente %s: %v\n", conn.RemoteAddr(), err)
        return
    }
    
    // Registrar cliente
    s.clientsMux.Lock()
    s.clients[clientInfo.ID] = clientInfo
    s.clientsMux.Unlock()
    
    fmt.Printf("[+] Cliente registrado: %s\n", clientInfo.ID)
    fmt.Printf("    Dirección pública: %s:%d\n", clientInfo.PublicIP, clientInfo.PublicPort)
    fmt.Printf("    Dirección local: %s:%d\n", clientInfo.LocalIP, clientInfo.LocalPort)
    
    // Mostrar lista de clientes conectados
    s.clientsMux.RLock()
    fmt.Printf("\nClientes conectados (%d):\n", len(s.clients))
    for id, client := range s.clients {
        fmt.Printf("- %s (%s:%d)\n", id, client.PublicIP, client.PublicPort)
    }
    s.clientsMux.RUnlock()
    
    // Enviar lista de clientes
    encoder := json.NewEncoder(conn)
    s.clientsMux.RLock()
    if err := encoder.Encode(s.clients); err != nil {
        fmt.Printf("[-] Error enviando lista de clientes a %s: %v\n", clientInfo.ID, err)
    }
    s.clientsMux.RUnlock()
}

func getLocalIP() string {
    addrs, err := net.InterfaceAddrs()
    if err != nil {
        return "No se pudo obtener la IP"
    }
    
    for _, addr := range addrs {
        if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
            if ipnet.IP.To4() != nil {
                return ipnet.IP.String()
            }
        }
    }
    return "No se encontró IP"
}

const DEFAULT_PORT = "6868"

func main() {
    fmt.Println("=== Servidor Rendezvous P2P ===")
    servidor := NewRendezvousServer()
    if err := servidor.Start(); err != nil {
        fmt.Printf("Error iniciando servidor: %v\n", err)
        os.Exit(1)
    }
} 