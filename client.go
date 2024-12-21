package main

import (
    "bufio"
    "fmt"
    "net"
    "os"
    "os/signal"
    "strings"
    "syscall"
    "time"
)

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
    // Conectar al servidor
    if err := c.conectarAlServidor(); err != nil {
        return err
    }
    defer c.conn.Close()
    
    // Iniciar sesión
    if err := c.iniciarSesion(); err != nil {
        return err
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