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
    peerConn net.Conn  // Conexión P2P principal
    udpConn  net.Conn  // Para datos del juego
    tcpConn  net.Conn  // Para datos importantes

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
    return c.connectToPeer(fmt.Sprintf("%s:%d", peerInfo[1], peerUDPPort))
}

func (c *GameClient) connectToPeer(peerAddr string) error {
    nat := NewNATTraversal()
    
    // Intentar establecer conexión P2P
    conn, err := nat.Connect(peerAddr)
    if err != nil {
        return err
    }
    
    c.peerConn = conn
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

func (c *GameClient) discoverServer() (string, error) {
    // Lista de métodos de descubrimiento
    methods := []func() (string, error){
        c.discoverByBroadcast,
        c.discoverByLocalScan,
        c.discoverByCommonIPs,
    }

    // Intentar cada método
    var lastError error
    for _, method := range methods {
        serverAddr, err := method()
        if err == nil {
            return serverAddr, nil
        }
        lastError = err
        fmt.Printf("Método de descubrimiento falló: %v\n", err)
    }

    return "", fmt.Errorf("no se pudo encontrar el servidor: %v", lastError)
}

func (c *GameClient) discoverByBroadcast() (string, error) {
    fmt.Println("Intentando descubrir por broadcast...")
    
    // Crear socket UDP
    addr, err := net.ResolveUDPAddr("udp4", ":0")
    if err != nil {
        return "", err
    }
    
    conn, err := net.ListenUDP("udp4", addr)
    if err != nil {
        return "", err
    }
    defer conn.Close()

    // Habilitar broadcast
    conn.SetWriteBuffer(1024)
    
    // Enviar broadcast a todas las interfaces de red
    interfaces, err := net.Interfaces()
    if err != nil {
        return "", err
    }

    message := []byte("DISCOVER_GAME_SERVER")
    for _, iface := range interfaces {
        addrs, err := iface.Addrs()
        if err != nil {
            continue
        }

        for _, addr := range addrs {
            if ipnet, ok := addr.(*net.IPNet); ok {
                if ipv4 := ipnet.IP.To4(); ipv4 != nil {
                    // Obtener dirección de broadcast para esta red
                    broadcast := getBroadcastAddress(ipnet)
                    broadcastAddr := &net.UDPAddr{
                        IP:   broadcast,
                        Port: 5001,
                    }
                    
                    // Enviar mensaje de descubrimiento
                    conn.WriteToUDP(message, broadcastAddr)
                }
            }
        }
    }

    // Esperar respuesta
    buffer := make([]byte, 1024)
    conn.SetReadDeadline(time.Now().Add(2 * time.Second))
    n, _, err := conn.ReadFromUDP(buffer)
    if err != nil {
        return "", err
    }

    return string(buffer[:n]), nil
}

func getBroadcastAddress(n *net.IPNet) net.IP {
    if len(n.IP) == 4 {
        mask := n.Mask
        broadcast := make(net.IP, 4)
        for i := range broadcast {
            broadcast[i] = n.IP[i] | ^mask[i]
        }
        return broadcast
    }
    return nil
}

func (c *GameClient) discoverByLocalScan() (string, error) {
    fmt.Println("Escaneando red local...")
    
    // Obtener IP local
    localIP := c.getLocalIPPrefix()
    if localIP == "" {
        return "", fmt.Errorf("no se pudo obtener IP local")
    }

    // Escanear IPs en la red local
    for i := 1; i < 255; i++ {
        targetIP := fmt.Sprintf("%s%d", localIP, i)
        if c.tryServerConnection(targetIP) {
            return fmt.Sprintf("%s:5000", targetIP), nil
        }
    }

    return "", fmt.Errorf("no se encontró servidor en la red local")
}

func (c *GameClient) getLocalIPPrefix() string {
    addrs, err := net.InterfaceAddrs()
    if err != nil {
        return ""
    }

    for _, addr := range addrs {
        if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
            if ipv4 := ipnet.IP.To4(); ipv4 != nil {
                // Devolver los primeros 3 octetos de la IP
                parts := strings.Split(ipv4.String(), ".")
                if len(parts) == 4 {
                    return fmt.Sprintf("%s.%s.%s.", parts[0], parts[1], parts[2])
                }
            }
        }
    }
    return ""
}

func (c *GameClient) discoverByCommonIPs() (string, error) {
    fmt.Println("Probando IPs comunes...")
    
    // Obtener el prefijo de la red local
    localPrefix := c.getLocalIPPrefix()
    
    commonIPs := []string{
        "127.0.0.1",          // localhost
        "192.168.1.1",        // Router común
        "192.168.0.1",        // Router común
        "192.168.68.109",     // Tu IP actual
    }

    // Agregar IPs de la red local
    if localPrefix != "" {
        for i := 1; i < 10; i++ {
            commonIPs = append(commonIPs, fmt.Sprintf("%s%d", localPrefix, i))
        }
    }

    for _, ip := range commonIPs {
        fmt.Printf("Probando %s...\n", ip)
        if c.tryServerConnection(ip) {
            return fmt.Sprintf("%s:5000", ip), nil
        }
    }

    return "", fmt.Errorf("no se encontró servidor en IPs comunes")
}

func (c *GameClient) tryServerConnection(ip string) bool {
    addr := fmt.Sprintf("%s:5000", ip)
    conn, err := net.DialTimeout("tcp", addr, time.Second)
    if err != nil {
        fmt.Printf("No se pudo conectar a %s: %v\n", addr, err)
        return false
    }
    conn.Close()
    fmt.Printf("¡Servidor encontrado en %s!\n", addr)
    return true
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

    // Permitir especificar IP manualmente
    fmt.Print("Presione Enter para buscar automáticamente o ingrese la IP del servidor: ")
    var input string
    fmt.Scanln(&input)

    var serverAddr string
    var err error

    if input != "" {
        // Usar IP ingresada
        if !strings.Contains(input, ":") {
            input = input + ":5000"
        }
        serverAddr = input
    } else {
        // Descubrir automáticamente
        serverAddr, err = client.discoverServer()
        if err != nil {
            fmt.Printf("Error descubriendo servidor: %v\n", err)
            fmt.Println("\nPresiona Enter para salir...")
            fmt.Scanln()
            return
        }
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