package main

import (
    "bufio"
    "encoding/json"
    "fmt"
    "net"
    "os"
    "os/signal"
    "strings"
    "syscall"
    "time"
)

const (
    RECONNECT_DELAY   = 5 * time.Second
    MAX_RETRIES       = 3
    DEFAULT_PORT      = "6868"  // Puerto para el servidor rendezvous
)

type ClientInfo struct {
    ID         string
    PublicIP   string
    PublicPort int
    LocalIP    string
    LocalPort  int
}

type Cliente struct {
    conn     net.Conn
    nombre   string
    servidor string
}

func NewCliente() *Cliente {
    return &Cliente{}
}

func (c *Cliente) conectarAlServidor() error {
    reader := bufio.NewReader(os.Stdin)
    
    fmt.Println("\n=== Cliente P2P ===")
    
    for {
        fmt.Print("Ingrese la dirección IP del servidor: ")
        ip, _ := reader.ReadString('\n')
        ip = strings.TrimSpace(ip)
        
        // Verificar si la dirección ya incluye el puerto
        if !strings.Contains(ip, ":") {
            ip = ip + ":8080"
        }
        
        fmt.Printf("\nIntentando conectar a %s...\n", ip)
        
        // Configurar el dialer con timeout más corto
        dialer := net.Dialer{
            Timeout: 5 * time.Second,
            KeepAlive: 30 * time.Second,
        }
        
        conn, err := dialer.Dial("tcp", ip)
        if err != nil {
            fmt.Printf("Error detallado al conectar: %v\n", err)
            fmt.Println("\nPosibles causas:")
            fmt.Println("1. El servidor no está ejecutándose")
            fmt.Println("2. La dirección IP es incorrecta")
            fmt.Println("3. El puerto 8080 está bloqueado")
            fmt.Println("4. Firewall está bloqueando la conexión")
            
            fmt.Print("\n¿Desea intentar de nuevo? (s/n): ")
            retry, _ := reader.ReadString('\n')
            if strings.ToLower(strings.TrimSpace(retry)) != "s" {
                return fmt.Errorf("conexión cancelada por el usuario")
            }
            continue
        }
        
        // Configurar TCP keep-alive
        tcpConn := conn.(*net.TCPConn)
        tcpConn.SetKeepAlive(true)
        tcpConn.SetKeepAlivePeriod(30 * time.Second)
        
        fmt.Println("Conexión establecida!")
        fmt.Printf("Conectado desde %v a %v\n", conn.LocalAddr(), conn.RemoteAddr())
        
        c.conn = conn
        c.servidor = ip
        break
    }
    
    return nil
}

func (c *Cliente) iniciarSesion() error {
    reader := bufio.NewReader(c.conn)
    
    // Enviar ping inicial
    fmt.Println("\n[PING] Enviando ping inicial al servidor...")
    _, err := fmt.Fprintf(c.conn, "PING\n")
    if err != nil {
        return fmt.Errorf("error al enviar ping: %v", err)
    }

    // Esperar pong
    fmt.Println("[PONG] Esperando respuesta del servidor...")
    respuesta, err := reader.ReadString('\n')
    if err != nil {
        return fmt.Errorf("error al leer pong: %v", err)
    }

    if strings.TrimSpace(respuesta) != "PONG" {
        return fmt.Errorf("respuesta incorrecta del servidor: %s", respuesta)
    }

    fmt.Printf("[PONG] Recibido PONG del servidor (%s)\n", c.conn.RemoteAddr())
    fmt.Println("[OK] Conexión verificada!")
    
    // Leer solicitud de nombre
    prompt, err := reader.ReadString('\n')
    if err != nil {
        return fmt.Errorf("error al leer del servidor (prompt): %v", err)
    }
    
    fmt.Print("\nServer> ", prompt)
    
    // Leer nombre del usuario desde la consola
    fmt.Print("Tu nombre> ")
    nombre, err := bufio.NewReader(os.Stdin).ReadString('\n')
    if err != nil {
        return fmt.Errorf("error al leer nombre: %v", err)
    }
    
    // Enviar nombre al servidor
    c.nombre = strings.TrimSpace(nombre)
    _, err = fmt.Fprintf(c.conn, "%s\n", c.nombre)
    if err != nil {
        return fmt.Errorf("error al enviar nombre: %v", err)
    }

    fmt.Println("Nombre enviado, esperando confirmación...")

    // Leer y mostrar el mensaje de bienvenida línea por línea
    for {
        linea, err := reader.ReadString('\n')
        if err != nil {
            return fmt.Errorf("error al leer bienvenida: %v", err)
        }
        fmt.Print(linea)
        
        // Si encontramos una línea vacía después de contenido, terminamos
        if strings.TrimSpace(linea) == "" {
            break
        }
    }
    
    return nil
}

func (c *Cliente) mantenerConexion() {
    reader := bufio.NewReader(c.conn)
    for {
        mensaje, err := reader.ReadString('\n')
        if err != nil {
            fmt.Println("\n[!] Error en la conexión:", err)
            c.conn.Close()
            os.Exit(1)
            return
        }

        if strings.TrimSpace(mensaje) == "PING" {
            // Responder con PONG
            fmt.Printf("[PING] Recibido PING del servidor (%s)\n", c.conn.RemoteAddr())
            _, err := fmt.Fprintf(c.conn, "PONG\n")
            if err != nil {
                fmt.Println("\n[!] Error al enviar PONG:", err)
                c.conn.Close()
                os.Exit(1)
                return
            }
            fmt.Printf("[PONG] PONG enviado al servidor\n")
        } else {
            // Es un mensaje normal
            fmt.Print(mensaje)
        }
    }
}

func (c *Cliente) enviarMensajes() {
    scanner := bufio.NewScanner(os.Stdin)
    for scanner.Scan() {
        mensaje := scanner.Text()
        if mensaje == "" {
            continue
        }
        
        _, err := fmt.Fprintf(c.conn, "%s\n", mensaje)
        if err != nil {
            fmt.Println("\n[!] Error al enviar mensaje:", err)
            return
        }
    }
}

func (c *Cliente) Iniciar() error {
    // Primero conectar al servidor de chat normal
    if err := c.conectarAlServidor(); err != nil {
        return err
    }
    defer c.conn.Close()
    
    // Iniciar sesión para obtener el nombre
    if err := c.iniciarSesion(); err != nil {
        return err
    }
    
    // Ahora intentar conectar al servidor rendezvous
    var retries int
    for {
        err := c.connectToRendezvous()
        if err == nil {
            break
        }
        
        retries++
        if retries >= MAX_RETRIES {
            fmt.Printf("No se pudo conectar al servidor rendezvous después de %d intentos\n", MAX_RETRIES)
            break // Continuamos sin P2P
        }
        
        fmt.Printf("Error conectando al servidor rendezvous: %v\n", err)
        fmt.Printf("Reintentando en %v...\n", RECONNECT_DELAY)
        time.Sleep(RECONNECT_DELAY)
    }
    
    // Configurar cierre limpio
    sigChan := make(chan os.Signal, 1)
    signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
    
    go func() {
        <-sigChan
        fmt.Println("\n\nCerrando cliente...")
        c.conn.Close()
        os.Exit(0)
    }()
    
    fmt.Println("\nConectado al servidor P2P")
    fmt.Println("Escriba sus mensajes y presione Enter para enviar")
    fmt.Println("Para salir, presione Ctrl+C")
    fmt.Println("----------------------------------------")
    
    // Iniciar rutinas de envío y recepción
    go c.mantenerConexion()
    c.enviarMensajes()
    
    return nil
}

func main() {
    cliente := NewCliente()
    if err := cliente.Iniciar(); err != nil {
        fmt.Printf("Error: %v\n", err)
        fmt.Println("Presione Enter para salir...")
        bufio.NewReader(os.Stdin).ReadString('\n')
        os.Exit(1)
    }
}

type PeerConnection struct {
    ID        string
    PublicIP  string
    PublicPort int
    LocalIP   string
    LocalPort int
    conn      net.Conn
}

func (c *Cliente) connectToRendezvous() error {
    reader := bufio.NewReader(os.Stdin)
    
    fmt.Println("\n=== Conexión al Servidor Rendezvous ===")
    
    fmt.Print("Ingrese la dirección IP del servidor rendezvous: ")
    ip, _ := reader.ReadString('\n')
    ip = strings.TrimSpace(ip)
    
    // Verificar si la dirección ya incluye el puerto
    if !strings.Contains(ip, ":") {
        ip = ip + ":" + DEFAULT_PORT
    }
    
    fmt.Printf("\nIntentando conectar al servidor rendezvous en %s...\n", ip)
    
    conn, err := net.Dial("tcp", ip)
    if err != nil {
        return fmt.Errorf("error conectando al servidor rendezvous: %v", err)
    }
    
    // Enviar información local
    localAddr := conn.LocalAddr().(*net.TCPAddr)
    info := ClientInfo{
        ID:        c.nombre,
        LocalIP:   localAddr.IP.String(),
        LocalPort: localAddr.Port,
    }
    
    encoder := json.NewEncoder(conn)
    if err := encoder.Encode(info); err != nil {
        return err
    }
    
    // Recibir lista de peers
    decoder := json.NewDecoder(conn)
    peers := make(map[string]*ClientInfo)
    if err := decoder.Decode(&peers); err != nil {
        return err
    }
    
    fmt.Printf("Conectado al servidor rendezvous. Peers disponibles: %d\n", len(peers))
    
    // Intentar conectar con cada peer
    for id, peer := range peers {
        if id != c.nombre {
            go c.tryConnectToPeer(peer)
        }
    }
    
    return nil
}

func (c *Cliente) tryConnectToPeer(peer *ClientInfo) {
    // Intentar conexión directa
    conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", peer.PublicIP, peer.PublicPort), 5*time.Second)
    if err != nil {
        // Si falla, intentar hole punching
        c.holePunching(peer)
        return
    }
    
    c.handlePeerConnection(conn, peer.ID)
}

func (c *Cliente) holePunching(peer *ClientInfo) {
    // Crear socket UDP local
    localAddr := &net.UDPAddr{IP: net.ParseIP("0.0.0.0"), Port: 0}
    conn, err := net.ListenUDP("udp", localAddr)
    if err != nil {
        return
    }
    
    // Enviar paquetes UDP al peer (público y local)
    peerPublic := &net.UDPAddr{IP: net.ParseIP(peer.PublicIP), Port: peer.PublicPort}
    peerLocal := &net.UDPAddr{IP: net.ParseIP(peer.LocalIP), Port: peer.LocalPort}
    
    // Enviar paquetes para abrir el NAT
    conn.WriteToUDP([]byte("hole-punching"), peerPublic)
    conn.WriteToUDP([]byte("hole-punching"), peerLocal)
    
    // Esperar respuesta
    buffer := make([]byte, 1024)
    conn.SetReadDeadline(time.Now().Add(5 * time.Second))
    
    _, addr, err := conn.ReadFromUDP(buffer)
    if err != nil {
        return
    }
    
    // Establecer conexión TCP después del hole punching
    tcpConn, err := net.DialTCP("tcp", nil, &net.TCPAddr{
        IP:   addr.IP,
        Port: addr.Port,
    })
    
    if err == nil {
        c.handlePeerConnection(tcpConn, peer.ID)
    }
}

func (c *Cliente) handlePeerConnection(conn net.Conn, peerID string) {
    fmt.Printf("Conexión P2P establecida con %s (%s)\n", peerID, conn.RemoteAddr())
    
    // Crear un nuevo reader para esta conexión
    reader := bufio.NewReader(conn)
    
    // Goroutine para recibir mensajes
    go func() {
        for {
            mensaje, err := reader.ReadString('\n')
            if err != nil {
                fmt.Printf("Conexión con peer %s cerrada\n", peerID)
                conn.Close()
                return
            }
            fmt.Printf("[%s] %s", peerID, mensaje)
        }
    }()
} 