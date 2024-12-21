package main

import (
    "fmt"
    "net"
    "sync"
    "os"
    "strings"
    "strconv"
    "time"
    "os/signal"
)

// Servidor de matchmaking y señalización solamente
type GameServer struct {
    // Para emparejar jugadores
    waitingPlayers map[string]*Player
    playersMux     sync.RWMutex

    // Para señalización inicial
    signalChan     map[string]chan []byte
    signalMux      sync.RWMutex

    // Para reenvío cuando falla P2P
    relayConns map[string]net.Conn
}

type Player struct {
    ID        string
    PublicIP  string
    LocalIP   string
    UDPPort   int  // Para datos del juego
    TCPPort   int  // Para datos importantes
    GameType  string // Tipo de juego que busca
}

func NewGameServer() *GameServer {
    return &GameServer{
        waitingPlayers: make(map[string]*Player),
        signalChan:    make(map[string]chan []byte),
        relayConns:   make(map[string]net.Conn),
    }
}

func (s *GameServer) Start() error {
    // Iniciar servicio de descubrimiento
    go s.startDiscoveryService()

    // Manejar señal de interrupción
    sigChan := make(chan os.Signal, 1)
    signal.Notify(sigChan, os.Interrupt)
    go func() {
        <-sigChan
        fmt.Println("\nCerrando servidor...")
        s.cleanup()
        os.Exit(0)
    }()

    // 1. Servidor STUN para ayudar a los clientes a descubrir su IP pública
    go s.startSTUNServer()

    // 2. Servidor de matchmaking (TCP)
    listener, err := net.Listen("tcp", ":5000")
    if err != nil {
        return err
    }

    fmt.Println("Servidor de juegos escuchando en :5000")
    fmt.Println("Este servidor SOLO maneja:")
    fmt.Println("- Matchmaking (emparejar jugadores)")
    fmt.Println("- Señalización inicial")
    fmt.Println("- Descubrimiento de IP (STUN)")
    fmt.Println("El tráfico del juego es P2P directo entre jugadores")

    for {
        conn, err := listener.Accept()
        if err != nil {
            continue
        }
        go s.handlePlayer(conn)
    }
}

func (s *GameServer) handlePlayer(conn net.Conn) {
    // 1. Recibir info del jugador
    buffer := make([]byte, 1024)
    n, err := conn.Read(buffer)
    if err != nil {
        fmt.Printf("Error leyendo info del jugador: %v\n", err)
        conn.Close()
        return
    }

    // Parsear la información recibida (formato: "localIP,publicIP,udpPort,tcpPort")
    info := strings.Split(string(buffer[:n]), ",")
    if len(info) < 4 {
        fmt.Println("Información de jugador incompleta")
        conn.Close()
        return
    }

    udpPort, _ := strconv.Atoi(strings.TrimSpace(info[2]))
    tcpPort, _ := strconv.Atoi(strings.TrimSpace(info[3]))

    player := &Player{
        ID:        conn.RemoteAddr().String(), // Usar la dirección como ID temporal
        LocalIP:   info[0],
        PublicIP:  info[1],
        UDPPort:   udpPort,
        TCPPort:   tcpPort,
        GameType:  "default", // Por ahora usamos un tipo por defecto
    }

    fmt.Printf("\nNuevo jugador conectado:\n")
    fmt.Printf("ID: %s\n", player.ID)
    fmt.Printf("IP Local: %s\n", player.LocalIP)
    fmt.Printf("IP Pública: %s\n", player.PublicIP)
    fmt.Printf("Puerto UDP: %d\n", player.UDPPort)
    fmt.Printf("Puerto TCP: %d\n", player.TCPPort)

    // Crear canal de señalización para este jugador
    s.signalMux.Lock()
    s.signalChan[player.ID] = make(chan []byte, 1)
    s.signalMux.Unlock()

    // Buscar oponente
    if opponent := s.findOpponent(player); opponent != nil {
        fmt.Printf("\n¡Emparejamiento encontrado!\n")
        fmt.Printf("Jugador 1: %s\n", player.ID)
        fmt.Printf("Jugador 2: %s\n", opponent.ID)
        
        // Si los clientes están en diferentes redes, usar relay
        if !sameNetwork(player.PublicIP, opponent.PublicIP) {
            fmt.Printf("Clientes en diferentes redes, usando relay\n")
            s.setupRelay(player, opponent)
        }

        // Enviar info del oponente al jugador actual
        peerInfo := fmt.Sprintf("%s,%s,%d,%d\n", 
            opponent.LocalIP, opponent.PublicIP, 
            opponent.UDPPort, opponent.TCPPort)
        conn.Write([]byte(peerInfo))

        // Enviar info del jugador actual al oponente
        s.signalMux.RLock()
        if ch, exists := s.signalChan[opponent.ID]; exists {
            ch <- []byte(fmt.Sprintf("%s,%s,%d,%d\n",
                player.LocalIP, player.PublicIP,
                player.UDPPort, player.TCPPort))
        }
        s.signalMux.RUnlock()
    } else {
        fmt.Printf("\nNo hay oponentes disponibles, agregando a la lista de espera...\n")
        s.addToWaitingList(player)
        
        // Esperar hasta que llegue un oponente o timeout
        select {
        case peerInfo := <-s.signalChan[player.ID]:
            conn.Write(peerInfo)
        case <-time.After(60 * time.Second):
            fmt.Printf("Timeout esperando oponente para %s\n", player.ID)
            s.removePlayer(player.ID)
            conn.Close()
            return
        }
    }
}

func (s *GameServer) startSTUNServer() {
    stunAddr := ":3478" // Puerto estándar STUN
    udpAddr, err := net.ResolveUDPAddr("udp", stunAddr)
    if err != nil {
        fmt.Printf("Error iniciando servidor STUN: %v\n", err)
        return
    }

    conn, err := net.ListenUDP("udp", udpAddr)
    if err != nil {
        fmt.Printf("Error escuchando UDP: %v\n", err)
        return
    }
    defer conn.Close()

    fmt.Printf("Servidor STUN escuchando en %s\n", stunAddr)

    buffer := make([]byte, 1024)
    for {
        n, remoteAddr, err := conn.ReadFromUDP(buffer)
        if err != nil {
            continue
        }
        
        // Responder con la IP pública del cliente
        response := []byte(remoteAddr.IP.String())
        conn.WriteToUDP(response, remoteAddr)
        
        fmt.Printf("Cliente STUN: %s (bytes: %d)\n", remoteAddr, n)
    }
}

func (s *GameServer) findOpponent(player *Player) *Player {
    s.playersMux.Lock()
    defer s.playersMux.Unlock()

    // Buscar jugador esperando el mismo tipo de juego
    for id, waiting := range s.waitingPlayers {
        if waiting.GameType == player.GameType {
            delete(s.waitingPlayers, id)
            return waiting
        }
    }
    return nil
}

func (s *GameServer) addToWaitingList(player *Player) {
    s.playersMux.Lock()
    s.waitingPlayers[player.ID] = player
    s.playersMux.Unlock()
    
    fmt.Printf("Jugador %s esperando partida (%s)\n", player.ID, player.GameType)
}

func (s *GameServer) removePlayer(playerID string) {
    s.playersMux.Lock()
    delete(s.waitingPlayers, playerID)
    s.playersMux.Unlock()

    s.signalMux.Lock()
    if ch, exists := s.signalChan[playerID]; exists {
        close(ch)
        delete(s.signalChan, playerID)
    }
    s.signalMux.Unlock()
}

func (s *GameServer) cleanup() {
    s.playersMux.Lock()
    for id := range s.waitingPlayers {
        s.removePlayer(id)
    }
    s.playersMux.Unlock()
}

func (s *GameServer) startDiscoveryService() {
    addr := &net.UDPAddr{
        IP:   net.IPv4(0, 0, 0, 0),
        Port: 5001, // Puerto para descubrimiento
    }
    
    conn, err := net.ListenUDP("udp", addr)
    if err != nil {
        fmt.Printf("Error iniciando servicio de descubrimiento: %v\n", err)
        return
    }
    defer conn.Close()

    fmt.Println("Servicio de descubrimiento escuchando en :5001")

    buffer := make([]byte, 1024)
    for {
        n, remoteAddr, err := conn.ReadFromUDP(buffer)
        if err != nil {
            continue
        }

        if string(buffer[:n]) == "DISCOVER_GAME_SERVER" {
            // Enviar IP y puerto del servidor
            response := fmt.Sprintf("%s:5000", getLocalIP())
            conn.WriteToUDP([]byte(response), remoteAddr)
            fmt.Printf("Cliente descubierto: %s\n", remoteAddr)
        }
    }
}

func getLocalIP() string {
    addrs, err := net.InterfaceAddrs()
    if err != nil {
        return "127.0.0.1"
    }
    
    // Primero buscar una IP no loopback
    for _, addr := range addrs {
        if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
            if ipnet.IP.To4() != nil {
                return ipnet.IP.String()
            }
        }
    }
    
    // Si no se encuentra, devolver localhost
    return "127.0.0.1"
}

func (s *GameServer) setupRelay(p1, p2 *Player) {
    // Crear túnel entre los jugadores
    relay1 := make(chan []byte, 1024)
    relay2 := make(chan []byte, 1024)

    // Reenviar datos entre jugadores
    go func() {
        for data := range relay1 {
            if conn, ok := s.relayConns[p2.ID]; ok {
                conn.Write(data)
            }
        }
    }()

    go func() {
        for data := range relay2 {
            if conn, ok := s.relayConns[p1.ID]; ok {
                conn.Write(data)
            }
        }
    }()
}

func sameNetwork(ip1, ip2 string) bool {
    // Comparar los primeros tres octetos
    parts1 := strings.Split(ip1, ".")
    parts2 := strings.Split(ip2, ".")
    
    if len(parts1) != 4 || len(parts2) != 4 {
        return false
    }
    
    return parts1[0] == parts2[0] && 
           parts1[1] == parts2[1] && 
           parts1[2] == parts2[2]
}

func main() {
    fmt.Println("\n=== Servidor de Juegos P2P ===")
    
    server := NewGameServer()
    if err := server.Start(); err != nil {
        fmt.Printf("Error iniciando servidor: %v\n", err)
        os.Exit(1)
    }
} 