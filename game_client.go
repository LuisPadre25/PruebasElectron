package main

import (
    "net"
    "fmt"
    "time"
    "strings"
    "strconv"
    "math/rand"
)

type GameClient struct {
    // Conexiones P2P
    udpConn net.Conn  // Para datos del juego
    tcpConn net.Conn  // Para datos importantes

    // Info local
    publicIP string
    localIP  string
    udpPort  int
    tcpPort  int
}

func (c *GameClient) Connect(serverAddr string) error {
    // 1. Descubrir IPs usando STUN
    if err := c.discoverAddresses(); err != nil {
        return err
    }

    // 2. Conectar al servidor de matchmaking
    conn, err := net.Dial("tcp", serverAddr)
    if err != nil {
        return err
    }
    defer conn.Close() // Cerrar la conexión cuando terminemos

    // 3. Enviar info y esperar oponente
    playerInfo := fmt.Sprintf("%s,%s,%d,%d\n", c.localIP, c.publicIP, c.udpPort, c.tcpPort)
    if _, err := conn.Write([]byte(playerInfo)); err != nil {
        return fmt.Errorf("error enviando info: %v", err)
    }

    fmt.Println("Esperando oponente...")

    // 4. Recibir info del oponente
    buffer := make([]byte, 1024)
    n, err := conn.Read(buffer)
    if err != nil {
        return fmt.Errorf("error recibiendo info del oponente: %v", err)
    }

    // Parsear info del oponente
    peerInfo := strings.Split(string(buffer[:n]), ",")
    if len(peerInfo) < 4 {
        return fmt.Errorf("información del oponente incompleta")
    }

    peerUDPPort, _ := strconv.Atoi(strings.TrimSpace(peerInfo[2]))
    peerTCPPort, _ := strconv.Atoi(strings.TrimSpace(peerInfo[3]))

    fmt.Printf("\nOponente encontrado:\n")
    fmt.Printf("IP Local: %s\n", peerInfo[0])
    fmt.Printf("IP Pública: %s\n", peerInfo[1])
    fmt.Printf("Puerto UDP: %d\n", peerUDPPort)
    fmt.Printf("Puerto TCP: %d\n", peerTCPPort)

    // Intentar establecer conexión P2P
    return c.establishP2PConnection()
}

func (c *GameClient) establishP2PConnection() error {
    // Primero crear los listeners
    if err := c.createListeners(); err != nil {
        return fmt.Errorf("error creando listeners: %v", err)
    }

    fmt.Println("Listeners creados, esperando conexiones...")

    // Luego intentar conectar al otro peer
    if err := c.connectToPeer(); err != nil {
        return fmt.Errorf("error conectando al peer: %v", err)
    }

    return nil
}

func (c *GameClient) createListeners() error {
    // Crear listener UDP
    udpAddr := &net.UDPAddr{
        IP:   net.ParseIP("0.0.0.0"),
        Port: c.udpPort,
    }
    udpListener, err := net.ListenUDP("udp", udpAddr)
    if err != nil {
        return fmt.Errorf("error creando listener UDP: %v", err)
    }

    // Crear listener TCP
    tcpAddr := &net.TCPAddr{
        IP:   net.ParseIP("0.0.0.0"),
        Port: c.tcpPort,
    }
    tcpListener, err := net.ListenTCP("tcp", tcpAddr)
    if err != nil {
        return fmt.Errorf("error creando listener TCP: %v", err)
    }

    // Iniciar goroutines para aceptar conexiones
    go c.handleUDPConnections(udpListener)
    go c.handleTCPConnections(tcpListener)

    return nil
}

func (c *GameClient) handleUDPConnections(listener *net.UDPConn) {
    buffer := make([]byte, 1024)
    for {
        n, addr, err := listener.ReadFromUDP(buffer)
        if err != nil {
            fmt.Printf("Error leyendo UDP: %v\n", err)
            continue
        }
        fmt.Printf("UDP recibido de %v: %s\n", addr, string(buffer[:n]))
        
        // Responder al peer
        listener.WriteToUDP([]byte("pong"), addr)
    }
}

func (c *GameClient) handleTCPConnections(listener *net.TCPListener) {
    for {
        conn, err := listener.Accept()
        if err != nil {
            fmt.Printf("Error aceptando TCP: %v\n", err)
            continue
        }
        fmt.Printf("Nueva conexión TCP de %v\n", conn.RemoteAddr())
        
        go c.handleTCPConnection(conn)
    }
}

func (c *GameClient) handleTCPConnection(conn net.Conn) {
    defer conn.Close()
    buffer := make([]byte, 1024)
    for {
        n, err := conn.Read(buffer)
        if err != nil {
            fmt.Printf("Error leyendo TCP: %v\n", err)
            return
        }
        fmt.Printf("TCP recibido: %s\n", string(buffer[:n]))
    }
}

func (c *GameClient) connectToPeer() error {
    // Esperar un momento para que los listeners estén listos
    time.Sleep(1 * time.Second)

    // Intentar conexión UDP primero
    if err := c.connectUDP(); err != nil {
        fmt.Printf("Conexión UDP falló, intentando hole punching: %v\n", err)
        if err := c.udpHolePunching(); err != nil {
            return fmt.Errorf("hole punching falló: %v", err)
        }
    }

    // Intentar conexión TCP
    if err := c.connectTCP(); err != nil {
        return fmt.Errorf("conexión TCP falló: %v", err)
    }

    return nil
}

func (c *GameClient) SendGameData(data []byte) error {
    // Enviar datos del juego por UDP directamente al otro jugador
    _, err := c.udpConn.Write(data)
    return err
}

func (c *GameClient) SendReliableData(data []byte) error {
    // Enviar datos importantes por TCP
    _, err := c.tcpConn.Write(data)
    return err
}

func (c *GameClient) discoverAddresses() error {
    // Obtener IP local
    addrs, err := net.InterfaceAddrs()
    if err != nil {
        return err
    }
    
    for _, addr := range addrs {
        if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
            if ipnet.IP.To4() != nil {
                c.localIP = ipnet.IP.String()
                break
            }
        }
    }

    // Obtener IP pública usando STUN
    stunConn, err := net.Dial("udp", "stun.l.google.com:19302")
    if err != nil {
        return fmt.Errorf("error conectando a STUN: %v", err)
    }
    defer stunConn.Close()

    // Obtener la IP pública
    if addr, ok := stunConn.LocalAddr().(*net.UDPAddr); ok {
        c.publicIP = addr.IP.String()
        fmt.Printf("IP pública obtenida: %s\n", c.publicIP)
    } else {
        return fmt.Errorf("no se pudo obtener la IP pública")
    }

    fmt.Printf("IPs descubiertas - Local: %s, Pública: %s\n", c.localIP, c.publicIP)
    return nil
}

func (c *GameClient) connectUDP() error {
    // Intentar conexión UDP directa
    addr := fmt.Sprintf("%s:%d", c.publicIP, c.udpPort)
    conn, err := net.Dial("udp", addr)
    if err != nil {
        return err
    }
    
    c.udpConn = conn
    return nil
}

func (c *GameClient) udpHolePunching() error {
    // 1. Crear socket UDP local
    localAddr := &net.UDPAddr{IP: net.ParseIP("0.0.0.0"), Port: 0}
    conn, err := net.ListenUDP("udp", localAddr)
    if err != nil {
        return err
    }

    // 2. Enviar paquetes UDP al peer (público y local)
    remotePublic := &net.UDPAddr{IP: net.ParseIP(c.publicIP), Port: c.udpPort}
    remoteLocal := &net.UDPAddr{IP: net.ParseIP(c.localIP), Port: c.udpPort}

    // Enviar paquetes para abrir el NAT
    conn.WriteToUDP([]byte("punch"), remotePublic)
    conn.WriteToUDP([]byte("punch"), remoteLocal)

    // 3. Esperar respuesta
    buffer := make([]byte, 1024)
    conn.SetReadDeadline(time.Now().Add(5 * time.Second))
    
    _, addr, err := conn.ReadFromUDP(buffer)
    if err != nil {
        return err
    }

    c.udpConn = conn
    fmt.Printf("Conexión UDP establecida con %v\n", addr)
    return nil
}

func (c *GameClient) connectTCP() error {
    // Similar al UDP pero para TCP
    addr := fmt.Sprintf("%s:%d", c.publicIP, c.tcpPort)
    conn, err := net.Dial("tcp", addr)
    if err != nil {
        return err
    }
    
    c.tcpConn = conn
    return nil
}

func (c *GameClient) discoverServer() (string, error) {
    // Crear socket UDP para broadcast
    conn, err := net.DialUDP("udp", nil, &net.UDPAddr{
        IP:   net.IPv4(255, 255, 255, 255),
        Port: 5001,
    })
    if err != nil {
        return "", err
    }
    defer conn.Close()

    // Enviar mensaje de descubrimiento
    fmt.Println("Buscando servidor...")
    conn.Write([]byte("DISCOVER_GAME_SERVER"))

    // Esperar respuesta
    buffer := make([]byte, 1024)
    conn.SetReadDeadline(time.Now().Add(5 * time.Second))
    n, _, err := conn.ReadFromUDP(buffer)
    if err != nil {
        return "", fmt.Errorf("no se encontró servidor: %v", err)
    }

    serverAddr := string(buffer[:n])
    fmt.Printf("Servidor encontrado en %s\n", serverAddr)
    return serverAddr, nil
}

// Función principal para usar el cliente
func main() {
    fmt.Println("\n=== Cliente de Juegos P2P ===")
    
    // Generar puertos aleatorios entre 30000 y 40000
    udpPort := 30000 + rand.Intn(10000)
    tcpPort := udpPort + 1
    
    client := &GameClient{
        udpPort: udpPort,
        tcpPort: tcpPort,
    }

    fmt.Printf("Usando puertos - UDP: %d, TCP: %d\n", udpPort, tcpPort)

    // Descubrir servidor automáticamente
    serverAddr, err := client.discoverServer()
    if err != nil {
        fmt.Printf("Error descubriendo servidor: %v\n", err)
        fmt.Println("\nPresiona Enter para salir...")
        fmt.Scanln()
        return
    }

    // Conectar al servidor descubierto
    err = client.Connect(serverAddr)
    if err != nil {
        fmt.Printf("Error conectando: %v\n", err)
        fmt.Println("\nPresiona Enter para salir...")
        fmt.Scanln()
        return
    }

    fmt.Println("Conectado exitosamente!")
    
    fmt.Println("\nPresiona Enter para salir...")
    fmt.Scanln()
} 