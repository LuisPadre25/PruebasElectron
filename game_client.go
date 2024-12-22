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
    fmt.Printf("Iniciando hole punching con %s:%s\n", ip, port)
    
    // 1. Crear socket UDP local (usando 0.0.0.0 para escuchar en todas las interfaces)
    localAddr := &net.UDPAddr{
        IP:   net.IPv4zero,  // Cambiar esto
        Port: c.udpPort,
    }
    conn, err := net.ListenUDP("udp", localAddr)
    if err != nil {
        // Si falla, intentar con puerto aleatorio
        localAddr.Port = 0
        conn, err = net.ListenUDP("udp", localAddr)
        if err != nil {
            return fmt.Errorf("error creando socket: %v", err)
        }
    }
    c.peerConn = conn

    // Mostrar en qué puerto estamos escuchando
    localAddr = conn.LocalAddr().(*net.UDPAddr)
    fmt.Printf("Escuchando en puerto local: %d\n", localAddr.Port)

    // 2. Dirección del peer
    peerAddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%s", ip, port))
    if err != nil {
        return fmt.Errorf("error resolviendo peer: %v", err)
    }

    // 3. Enviar paquetes de hole punching (más intentos)
    fmt.Println("Enviando paquetes de hole punching...")
    done := make(chan bool)
    go func() {
        for i := 0; i < 10; i++ {  // Aumentar intentos
            msg := fmt.Sprintf("punch-%d", i)
            conn.WriteToUDP([]byte(msg), peerAddr)
            time.Sleep(200 * time.Millisecond)  // Más tiempo entre intentos
        }
        done <- true
    }()

    // 4. Esperar respuesta
    fmt.Println("Esperando respuesta del peer...")
    buffer := make([]byte, 1024)
    conn.SetReadDeadline(time.Now().Add(10 * time.Second))  // Más tiempo de espera

    // Canal para señalizar conexión exitosa
    success := make(chan bool)

    // Goroutine para leer respuestas
    go func() {
        for {
            n, remoteAddr, err := conn.ReadFromUDP(buffer)
            if err != nil {
                fmt.Printf("Error leyendo: %v\n", err)
                success <- false
                return
            }

            fmt.Printf("Recibido de %v: %s\n", remoteAddr, string(buffer[:n]))

            // 5. Enviar respuesta
            conn.WriteToUDP([]byte("¡Te escucho!"), remoteAddr)
            success <- true
            
            // 6. Seguir escuchando mensajes
            go func() {
                buffer := make([]byte, 1024)
                for {
                    n, addr, err := conn.ReadFromUDP(buffer)
                    if err != nil {
                        fmt.Printf("Error leyendo: %v\n", err)
                        return
                    }
                    fmt.Printf("Mensaje de %v: %s\n", addr, string(buffer[:n]))
                }
            }()

            return
        }
    }()

    // Esperar resultado
    select {
    case <-done:
        fmt.Println("Terminaron los intentos de hole punching")
    case result := <-success:
        if result {
            fmt.Println("¡Conexión P2P establecida!")
            return nil
        }
    case <-time.After(10 * time.Second):
        return fmt.Errorf("timeout esperando conexión P2P")
    }

    return fmt.Errorf("no se pudo establecer conexión P2P")
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