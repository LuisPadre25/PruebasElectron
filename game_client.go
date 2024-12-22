package main

import (
    "fmt"
    "net"
    "strings"
    "bytes"
    "time"
    "github.com/pion/stun/v2"
)

type GameClient struct {
    // Info de red
    localIP    string
    publicIP   string
    udpPort    int
    tcpPort    int

    // Conexiones
    serverConn net.Conn  // Conexión al servidor
    peerConn   net.Conn  // Conexión P2P con otro cliente
}

func NewGameClient(udpPort, tcpPort int) *GameClient {
    return &GameClient{
        udpPort: udpPort,
        tcpPort: tcpPort,
    }
}

// Descubrir IP pública usando STUN
func (c *GameClient) discoverIP(stunServer string) error {
    fmt.Printf("Conectando a servidor STUN %s...\n", stunServer)
    
    // Crear cliente STUN
    client, err := stun.Dial("udp4", stunServer)
    if err != nil {
        return fmt.Errorf("error STUN dial: %v", err)
    }
    defer client.Close()

    // Generar TransactionID único
    tid := stun.NewTransactionID()
    fmt.Printf("STUN: Usando TransactionID: %v\n", tid)

    // Enviar petición STUN
    message := stun.MustBuild(
        stun.TransactionID,
        stun.BindingRequest,
        stun.NewTransactionIDSetter(tid),
    )
    
    // Obtener respuesta con nuestra IP pública
    var xorAddr stun.XORMappedAddress
    gotIP := false

    if err := client.Do(message, func(res stun.Event) {
        fmt.Printf("STUN: Recibida respuesta: %+v\n", res)
        
        if res.Error != nil {
            err = res.Error
            return
        }

        // Verificar TransactionID
        if !bytes.Equal(tid[:], res.Message.TransactionID[:]) {
            fmt.Printf("STUN: TransactionID no coincide (esperado: %v, recibido: %v)\n",
                tid, res.Message.TransactionID)
            return
        }

        if getErr := xorAddr.GetFrom(res.Message); getErr != nil {
            err = getErr
            return
        }

        fmt.Printf("STUN: IP mapeada: %s:%d\n", xorAddr.IP, xorAddr.Port)
        c.publicIP = xorAddr.IP.String()
        gotIP = true
    }); err != nil {
        return fmt.Errorf("error STUN request: %v", err)
    }

    if !gotIP {
        return fmt.Errorf("no se recibió respuesta STUN válida")
    }

    // Obtener IP local
    addrs, err := net.InterfaceAddrs()
    if err != nil {
        return fmt.Errorf("error IP local: %v", err)
    }

    for _, addr := range addrs {
        if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
            if ipnet.IP.To4() != nil {
                c.localIP = ipnet.IP.String()
                break
            }
        }
    }

    fmt.Printf("IPs descubiertas:\n")
    fmt.Printf("Local: %s\n", c.localIP)
    fmt.Printf("Pública: %s\n", c.publicIP)
    return nil
}

// Conectar al servidor de matchmaking
func (c *GameClient) Connect(serverAddr string) error {
    fmt.Printf("Conectando a servidor de matchmaking %s...\n", serverAddr)
    
    // Conectar al servidor
    conn, err := net.Dial("tcp", serverAddr)
    if err != nil {
        return fmt.Errorf("error conectando: %v", err)
    }
    c.serverConn = conn

    // Enviar nuestra info
    info := fmt.Sprintf("%s,%s,%d,%d\n",
        c.localIP, c.publicIP, c.udpPort, c.tcpPort)
    
    if _, err := conn.Write([]byte(info)); err != nil {
        return fmt.Errorf("error enviando info: %v", err)
    }

    fmt.Println("Esperando oponente...")

    // Esperar info del oponente
    buffer := make([]byte, 1024)
    n, err := conn.Read(buffer)
    if err != nil {
        return fmt.Errorf("error recibiendo info: %v", err)
    }

    // Parsear info del oponente
    parts := strings.Split(strings.TrimSpace(string(buffer[:n])), ",")
    if len(parts) != 4 {
        return fmt.Errorf("formato inválido del oponente")
    }

    fmt.Printf("\nOponente encontrado:\n")
    fmt.Printf("IP Local: %s\n", parts[0])
    fmt.Printf("IP Pública: %s\n", parts[1])
    fmt.Printf("UDP: %s\n", parts[2])
    fmt.Printf("TCP: %s\n", parts[3])

    // Intentar conexión P2P
    return c.connectToPeer(parts[1], parts[2])
}

// Intentar conexión P2P con el otro cliente
func (c *GameClient) connectToPeer(ip, port string) error {
    fmt.Printf("Intentando conexión P2P a %s:%s\n", ip, port)
    
    conn, err := net.Dial("udp", fmt.Sprintf("%s:%s", ip, port))
    if err != nil {
        return fmt.Errorf("error P2P: %v", err)
    }
    c.peerConn = conn

    // Enviar mensaje de prueba
    fmt.Println("Enviando mensaje de prueba...")
    if _, err := conn.Write([]byte("¡Hola! ¿Me escuchas?")); err != nil {
        return fmt.Errorf("error enviando: %v", err)
    }

    // Esperar respuesta
    buffer := make([]byte, 1024)
    conn.SetReadDeadline(time.Now().Add(5 * time.Second))
    n, err := conn.Read(buffer)
    if err != nil {
        return fmt.Errorf("error recibiendo respuesta: %v", err)
    }

    fmt.Printf("Recibido del peer: %s\n", string(buffer[:n]))
    fmt.Println("¡Conexión P2P verificada!")

    // Iniciar goroutine para escuchar mensajes
    go func() {
        buffer := make([]byte, 1024)
        for {
            n, err := conn.Read(buffer)
            if err != nil {
                fmt.Printf("Error leyendo del peer: %v\n", err)
                return
            }
            fmt.Printf("Mensaje del peer: %s\n", string(buffer[:n]))
        }
    }()

    return nil
}

func main() {
    fmt.Println("\n=== Cliente P2P ===")
    
    // Usar puertos aleatorios para evitar conflictos
    client := NewGameClient(35000, 35001)

    // 1. Descubrir IP usando STUN (IP de tu servidor Vultr)
    if err := client.discoverIP("149.28.106.4:3478"); err != nil {
        fmt.Printf("Error STUN: %v\n", err)
        return
    }

    // 2. Conectar al servidor de matchmaking
    if err := client.Connect("149.28.106.4:5000"); err != nil {
        fmt.Printf("Error conexión: %v\n", err)
        return
    }

    fmt.Println("\nPresiona Enter para salir...")
    fmt.Scanln()
} 