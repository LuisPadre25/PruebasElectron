package main

import (
    "bufio"
    "fmt"
    "net"
    "os"
    "os/signal"
    "strings"
    "sync"
    "syscall"
    "time"
)

type Cliente struct {
    conn     net.Conn
    nombre   string
    lastPing time.Time
}

type Servidor struct {
    clientes     map[string]*Cliente
    clientesMux  sync.RWMutex
    listener     net.Listener
    cerrarCanal  chan struct{}
}

func NewServidor() *Servidor {
    return &Servidor{
        clientes:    make(map[string]*Cliente),
        cerrarCanal: make(chan struct{}),
    }
}

func (s *Servidor) agregarCliente(cliente *Cliente) {
    s.clientesMux.Lock()
    defer s.clientesMux.Unlock()
    s.clientes[cliente.conn.RemoteAddr().String()] = cliente
    fmt.Printf("\n[+] Nuevo cliente conectado: %s (%s)\n", cliente.nombre, cliente.conn.RemoteAddr().String())
    s.broadcastMensaje(fmt.Sprintf("Sistema: %s se ha unido al chat\n", cliente.nombre), cliente.conn)
}

func (s *Servidor) eliminarCliente(addr string) {
    s.clientesMux.Lock()
    defer s.clientesMux.Unlock()
    if cliente, existe := s.clientes[addr]; existe {
        fmt.Printf("\n[-] Cliente desconectado: %s (%s)\n", cliente.nombre, addr)
        s.broadcastMensaje(fmt.Sprintf("Sistema: %s se ha desconectado\n", cliente.nombre), nil)
        delete(s.clientes, addr)
    }
}

func (s *Servidor) broadcastMensaje(mensaje string, exceptConn net.Conn) {
    s.clientesMux.RLock()
    defer s.clientesMux.RUnlock()
    
    for _, cliente := range s.clientes {
        if cliente.conn != exceptConn {
            cliente.conn.Write([]byte(mensaje))
        }
    }
}

func (s *Servidor) monitorearClientes() {
    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()

    for {
        select {
        case <-ticker.C:
            s.clientesMux.Lock()
            ahora := time.Now()
            for addr, cliente := range s.clientes {
                if ahora.Sub(cliente.lastPing) > 1*time.Minute {
                    fmt.Printf("\n[!] Cliente %s (%s) no responde, desconectando...\n", cliente.nombre, addr)
                    cliente.conn.Close()
                    delete(s.clientes, addr)
                }
            }
            s.clientesMux.Unlock()
        case <-s.cerrarCanal:
            return
        }
    }
}

func (s *Servidor) mantenerConexion(cliente *Cliente) {
    ticker := time.NewTicker(15 * time.Second)
    defer ticker.Stop()

    for {
        select {
        case <-ticker.C:
            // Enviar ping al cliente
            fmt.Printf("[PING] Enviando ping a %s (%s)\n", cliente.nombre, cliente.conn.RemoteAddr())
            _, err := cliente.conn.Write([]byte("PING\n"))
            if err != nil {
                fmt.Printf("[ERROR] Error al enviar ping a %s: %v\n", cliente.nombre, err)
                cliente.conn.Close()
                return
            }

            // Esperar pong con timeout
            cliente.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
            reader := bufio.NewReader(cliente.conn)
            respuesta, err := reader.ReadString('\n')
            cliente.conn.SetReadDeadline(time.Time{}) // Quitar timeout

            if err != nil {
                fmt.Printf("[ERROR] No se recibió respuesta de %s: %v\n", cliente.nombre, err)
                cliente.conn.Close()
                return
            }

            if strings.TrimSpace(respuesta) != "PONG" {
                fmt.Printf("[ERROR] Respuesta incorrecta de %s: %s\n", cliente.nombre, respuesta)
                cliente.conn.Close()
                return
            }

            fmt.Printf("[PONG] Recibido pong de %s (%s)\n", cliente.nombre, cliente.conn.RemoteAddr())
            cliente.lastPing = time.Now()
        case <-s.cerrarCanal:
            return
        }
    }
}

func (s *Servidor) handleConnection(conn net.Conn) {
    reader := bufio.NewReader(conn)
    
    // Esperar ping inicial
    fmt.Printf("[PING] Esperando ping inicial de %s\n", conn.RemoteAddr())
    mensaje, err := reader.ReadString('\n')
    if err != nil {
        fmt.Printf("[ERROR] Error al leer ping inicial de %s: %v\n", conn.RemoteAddr(), err)
        conn.Close()
        return
    }

    if strings.TrimSpace(mensaje) != "PING" {
        fmt.Printf("[ERROR] Mensaje inicial incorrecto de %s: %s\n", conn.RemoteAddr(), mensaje)
        conn.Close()
        return
    }

    fmt.Printf("[PING] Ping inicial recibido de %s\n", conn.RemoteAddr())

    // Enviar pong
    fmt.Printf("[PONG] Enviando pong inicial a %s\n", conn.RemoteAddr())
    _, err = conn.Write([]byte("PONG\n"))
    if err != nil {
        fmt.Printf("[ERROR] Error al enviar pong inicial a %s: %v\n", conn.RemoteAddr(), err)
        conn.Close()
        return
    }

    // Solicitar nombre de usuario
    _, err = conn.Write([]byte("Por favor, ingrese su nombre: "))
    if err != nil {
        fmt.Printf("Error al solicitar nombre: %v\n", err)
        conn.Close()
        return
    }

    // Leer nombre sin timeout
    nombre, err := reader.ReadString('\n')
    if err != nil {
        fmt.Printf("Error al leer nombre: %v\n", err)
        conn.Close()
        return
    }
    
    nombre = strings.TrimSpace(nombre)
    if nombre == "" {
        fmt.Println("Nombre vacío recibido, cerrando conexión")
        conn.Close()
        return
    }

    cliente := &Cliente{
        conn:     conn,
        nombre:   nombre,
        lastPing: time.Now(),
    }
    
    s.agregarCliente(cliente)
    defer func() {
        s.eliminarCliente(conn.RemoteAddr().String())
        conn.Close()
    }()

    // Lista de usuarios conectados
    listaUsuarios := "Usuarios conectados:\n"
    s.clientesMux.RLock()
    for _, c := range s.clientes {
        listaUsuarios += fmt.Sprintf("- %s (%s)\n", c.nombre, c.conn.RemoteAddr())
    }
    s.clientesMux.RUnlock()

    // Enviar mensaje de bienvenida
    mensajeBienvenida := fmt.Sprintf(`
¡Bienvenido %s! Hay %d usuarios conectados.

%s
Comandos disponibles:
/usuarios - Muestra la lista de usuarios conectados
/ayuda    - Muestra esta ayuda
/privado <usuario> <mensaje> - Envía un mensaje privado
/salir    - Desconecta del servidor

`, nombre, len(s.clientes), listaUsuarios)
    
    _, err = conn.Write([]byte(mensajeBienvenida))
    if err != nil {
        fmt.Printf("Error al enviar bienvenida: %v\n", err)
        return
    }

    fmt.Printf("[+] Usuario %s conectado desde %s\n", nombre, conn.RemoteAddr())

    // Bucle principal de mensajes
    for {
        mensaje, err := reader.ReadString('\n')
        if err != nil {
            fmt.Printf("[-] Error leyendo mensaje de %s: %v\n", nombre, err)
            return
        }

        mensaje = strings.TrimSpace(mensaje)
        cliente.lastPing = time.Now()

        if mensaje == "" {
            continue
        }

        // Procesar comandos
        if strings.HasPrefix(mensaje, "/") {
            s.procesarComando(cliente, mensaje)
            continue
        }

        // Mensaje normal
        mensajeFormateado := fmt.Sprintf("[%s] %s: %s\n", time.Now().Format("15:04:05"), cliente.nombre, mensaje)
        fmt.Print(mensajeFormateado)
        s.broadcastMensaje(mensajeFormateado, conn)
    }

    go s.mantenerConexion(cliente)
}

// Agregar esta nueva función para procesar comandos
func (s *Servidor) procesarComando(cliente *Cliente, comando string) {
    partes := strings.Fields(comando)
    if len(partes) == 0 {
        return
    }

    switch partes[0] {
    case "/usuarios":
        s.clientesMux.RLock()
        mensaje := "Usuarios conectados:\n"
        for _, c := range s.clientes {
            mensaje += fmt.Sprintf("- %s (%s)\n", c.nombre, c.conn.RemoteAddr())
        }
        s.clientesMux.RUnlock()
        cliente.conn.Write([]byte(mensaje))

    case "/privado":
        if len(partes) < 3 {
            cliente.conn.Write([]byte("Uso: /privado <usuario> <mensaje>\n"))
            return
        }
        destinatario := partes[1]
        mensaje := strings.Join(partes[2:], " ")
        s.enviarMensajePrivado(cliente, destinatario, mensaje)

    case "/ayuda":
        ayuda := `
Comandos disponibles:
/usuarios - Muestra la lista de usuarios conectados
/privado <usuario> <mensaje> - Envía un mensaje privado
/ayuda    - Muestra esta ayuda
/salir    - Desconecta del servidor
`
        cliente.conn.Write([]byte(ayuda))

    case "/salir":
        cliente.conn.Close()
    }
}

// Agregar esta nueva función para mensajes privados
func (s *Servidor) enviarMensajePrivado(remitente *Cliente, destinatarioNombre, mensaje string) {
    s.clientesMux.RLock()
    defer s.clientesMux.RUnlock()

    for _, cliente := range s.clientes {
        if cliente.nombre == destinatarioNombre {
            mensajePrivado := fmt.Sprintf("[Privado de %s]: %s\n", remitente.nombre, mensaje)
            cliente.conn.Write([]byte(mensajePrivado))
            remitente.conn.Write([]byte(fmt.Sprintf("[Privado para %s]: %s\n", destinatarioNombre, mensaje)))
            return
        }
    }
    remitente.conn.Write([]byte(fmt.Sprintf("Usuario %s no encontrado\n", destinatarioNombre)))
}

func (s *Servidor) Iniciar() error {
    // Obtener todas las IPs disponibles
    fmt.Println("\n=== Servidor P2P ===")
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

    localIP := getLocalIP()
    fmt.Printf("\nIP principal del servidor: %s\n", localIP)
    fmt.Printf("Puerto: 8080\n")
    fmt.Printf("Los clientes deben conectarse usando: %s:8080\n", localIP)

    // Intentar escuchar en todas las interfaces
    fmt.Println("\nIniciando servidor...")
    s.listener, err = net.Listen("tcp", "0.0.0.0:8080")
    if err != nil {
        return fmt.Errorf("error al iniciar el servidor: %v", err)
    }

    fmt.Printf("Servidor escuchando en %s\n", s.listener.Addr().String())

    // Iniciar monitoreo de clientes
    go s.monitorearClientes()

    // Manejar señales de cierre
    sigChan := make(chan os.Signal, 1)
    signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

    go func() {
        <-sigChan
        fmt.Println("\n\nCerrando servidor...")
        close(s.cerrarCanal)
        s.listener.Close()
        
        // Cerrar todas las conexiones de clientes
        s.clientesMux.Lock()
        for _, cliente := range s.clientes {
            cliente.conn.Close()
        }
        s.clientesMux.Unlock()
    }()

    fmt.Println("\nServidor P2P iniciado y esperando conexiones...")
    fmt.Println("Presione Ctrl+C para cerrar el servidor")
    fmt.Println("----------------------------------------")

    // Aceptar conexiones
    for {
        conn, err := s.listener.Accept()
        if err != nil {
            select {
            case <-s.cerrarCanal:
                return nil
            default:
                fmt.Printf("Error aceptando conexión: %v\n", err)
                continue
            }
        }
        go s.handleConnection(conn)
    }
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

func main() {
    servidor := NewServidor()
    if err := servidor.Iniciar(); err != nil {
        fmt.Printf("Error: %v\n", err)
        os.Exit(1)
    }
} 