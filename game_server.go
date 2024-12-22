package main

import (
    "fmt"
    "net"
    "sync"
    "strings"
    "strconv"
    "github.com/pion/stun/v2"
)

// Jugador conectado al servidor
type Player struct {
    ID        string
    LocalIP   string
    PublicIP  string
    UDPPort   int
    TCPPort   int
    Conn      net.Conn
}

// Servidor de matchmaking y STUN
type Server struct {
    // Para matchmaking
    waitingPlayers map[string]*Player
    mutex          sync.RWMutex

    // Para STUN
    stunConn *net.UDPConn
}

func NewServer() *Server {
    return &Server{
        waitingPlayers: make(map[string]*Player),
    }
}

// Iniciar servidor STUN
func (s *Server) startSTUN() error {
    addr, err := net.ResolveUDPAddr("udp4", ":3478")
    if err != nil {
        return fmt.Errorf("error STUN addr: %v", err)
    }

    s.stunConn, err = net.ListenUDP("udp4", addr)
    if err != nil {
        return fmt.Errorf("error STUN listen: %v", err)
    }

    fmt.Printf("Servidor STUN escuchando en %v\n", s.stunConn.LocalAddr())

    go func() {
        buffer := make([]byte, 1024)
        for {
            n, remoteAddr, err := s.stunConn.ReadFromUDP(buffer)
            if err != nil {
                fmt.Printf("Error STUN read: %v\n", err)
                continue
            }

            fmt.Printf("STUN: Recibido %d bytes de %s\n", n, remoteAddr)

            message := &stun.Message{
                Raw: buffer[:n],
            }
            if err := message.Decode(); err != nil {
                fmt.Printf("Error STUN decode: %v\n", err)
                continue
            }

            if message.Type == stun.BindingRequest {
                fmt.Printf("STUN: Binding request de %s\n", remoteAddr)
                
                // Obtener el TransactionID del request
                tid := message.TransactionID
                fmt.Printf("STUN: TransactionID recibido: %v\n", tid)
                
                resp, err := stun.Build(
                    stun.NewType(stun.MethodBinding, stun.ClassSuccessResponse),
                    stun.Fingerprint,
                    &stun.XORMappedAddress{
                        IP:   remoteAddr.IP,
                        Port: remoteAddr.Port,
                    },
                    stun.NewTransactionIDSetter(tid), // Usar el mismo TransactionID
                )
                if err != nil {
                    fmt.Printf("Error STUN build: %v\n", err)
                    continue
                }

                fmt.Printf("STUN: Enviando respuesta con IP: %s:%d (TID: %v)\n", 
                    remoteAddr.IP, remoteAddr.Port, tid)
                
                if _, err := s.stunConn.WriteToUDP(resp.Raw, remoteAddr); err != nil {
                    fmt.Printf("Error STUN write: %v\n", err)
                    continue
                }

                fmt.Printf("STUN: Respuesta enviada a %s\n", remoteAddr)
            }
        }
    }()

    return nil
}

// Iniciar servidor de matchmaking
func (s *Server) Start() error {
    // Iniciar STUN primero
    if err := s.startSTUN(); err != nil {
        return err
    }

    // Iniciar servidor TCP para matchmaking
    listener, err := net.Listen("tcp", ":5000")
    if err != nil {
        return err
    }
    defer listener.Close()

    fmt.Println("=== Servidor P2P ===")
    fmt.Println("Matchmaking: :5000")
    fmt.Println("STUN: :3478")

    for {
        conn, err := listener.Accept()
        if err != nil {
            fmt.Printf("Error conexión: %v\n", err)
            continue
        }
        go s.handleConnection(conn)
    }
}

func (s *Server) handleConnection(conn net.Conn) {
    defer conn.Close()

    // Leer info del cliente
    buffer := make([]byte, 1024)
    n, err := conn.Read(buffer)
    if err != nil {
        fmt.Printf("Error lectura: %v\n", err)
        return
    }

    // Parsear info (formato: "localIP,publicIP,udpPort,tcpPort")
    parts := strings.Split(strings.TrimSpace(string(buffer[:n])), ",")
    if len(parts) != 4 {
        fmt.Printf("Formato inválido: %s\n", string(buffer[:n]))
        return
    }

    udpPort, _ := strconv.Atoi(parts[2])
    tcpPort, _ := strconv.Atoi(parts[3])

    player := &Player{
        ID:       conn.RemoteAddr().String(),
        LocalIP:  parts[0],
        PublicIP: parts[1],
        UDPPort:  udpPort,
        TCPPort:  tcpPort,
        Conn:     conn,
    }

    fmt.Printf("\nNuevo jugador:\n")
    fmt.Printf("ID: %s\n", player.ID)
    fmt.Printf("Local: %s\n", player.LocalIP)
    fmt.Printf("Public: %s\n", player.PublicIP)
    fmt.Printf("UDP: %d\n", player.UDPPort)
    fmt.Printf("TCP: %d\n", player.TCPPort)

    // Buscar oponente
    if opponent := s.findOpponent(player); opponent != nil {
        fmt.Printf("Match: %s <-> %s\n", player.ID, opponent.ID)
        s.matchPlayers(player, opponent)
    } else {
        fmt.Printf("Esperando: %s\n", player.ID)
        s.addToWaitingList(player)
        
        // Esperar hasta que llegue un oponente o el cliente se desconecte
        buffer := make([]byte, 1024)
        for {
            _, err := conn.Read(buffer)
            if err != nil {
                fmt.Printf("Cliente %s desconectado\n", player.ID)
                s.removePlayer(player.ID)
                return
            }
        }
    }
}

func (s *Server) matchPlayers(p1, p2 *Player) {
    // Enviar info de p2 a p1
    p1Info := fmt.Sprintf("%s,%s,%d,%d\n", 
        p2.LocalIP, p2.PublicIP, p2.UDPPort, p2.TCPPort)
    p1.Conn.Write([]byte(p1Info))

    // Enviar info de p1 a p2
    p2Info := fmt.Sprintf("%s,%s,%d,%d\n",
        p1.LocalIP, p1.PublicIP, p1.UDPPort, p1.TCPPort)
    p2.Conn.Write([]byte(p2Info))
}

func (s *Server) findOpponent(player *Player) *Player {
    s.mutex.Lock()
    defer s.mutex.Unlock()

    for id, opponent := range s.waitingPlayers {
        delete(s.waitingPlayers, id)
        return opponent
    }
    return nil
}

func (s *Server) addToWaitingList(player *Player) {
    s.mutex.Lock()
    s.waitingPlayers[player.ID] = player
    s.mutex.Unlock()
}

// Agregar esta función al Server
func (s *Server) removePlayer(playerID string) {
    s.mutex.Lock()
    defer s.mutex.Unlock()

    // Eliminar de la lista de espera
    delete(s.waitingPlayers, playerID)
    fmt.Printf("Jugador %s eliminado de la lista de espera\n", playerID)
}

func main() {
    server := NewServer()
    if err := server.Start(); err != nil {
        fmt.Printf("Error fatal: %v\n", err)
    }
} 