package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"syscall/js"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/transport/websocket"
	"github.com/multiformats/go-multiaddr"
)

type PeerInfo struct {
	ID        string   `json:"id"`
	Addresses []string `json:"addresses"`
	Name      string   `json:"name"`
	GameInfo  string   `json:"gameInfo,omitempty"`
}

var (
	node     host.Host
	ctx      context.Context
	peerName string
)

func logP2P(category string, message string, args ...interface{}) {
	console := js.Global().Get("console")
	fullMessage := fmt.Sprintf(message, args...)
	
	switch category {
	case "error":
		console.Call("error", "❌ [P2P]", fullMessage)
	case "warn":
		console.Call("warn", "⚠️ [P2P]", fullMessage)
	default:
		console.Call("log", "🔗 [P2P]", fullMessage)
	}
}

func getOutboundIP() string {
	// Intentar obtener todas las interfaces de red
	interfaces, err := net.Interfaces()
	if err != nil {
		logP2P("warn", "Error obteniendo interfaces de red: %v", err)
		return "127.0.0.1"
	}

	for _, iface := range interfaces {
		// Ignorar interfaces loopback y deshabilitadas
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}

		// Obtener direcciones de la interfaz
		addrs, err := iface.Addrs()
		if err != nil {
			logP2P("warn", "Error obteniendo direcciones para interfaz %s: %v", iface.Name, err)
			continue
		}

		for _, addr := range addrs {
			// Convertir a IP
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}

			// Verificar que sea IPv4 y no sea loopback
			if ip == nil || ip.IsLoopback() {
				continue
			}
			ip = ip.To4()
			if ip == nil {
				continue
			}

			logP2P("info", "Interfaz encontrada: %s, IP: %s", iface.Name, ip.String())
			return ip.String()
		}
	}

	// Si no se encuentra ninguna IP válida, intentar el método de conexión UDP
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		logP2P("warn", "Error en conexión UDP de prueba: %v", err)
		return "127.0.0.1"
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP.String()
}

// Mover tryGetIP fuera de getServerIP
func tryGetIP(done chan string, attempt int, maxRetries int, retryDelay time.Duration) {
	electronObj := js.Global().Get("electron")
	if electronObj.IsUndefined() || electronObj.IsNull() {
		logP2P("warn", "Objeto electron no disponible")
		done <- "127.0.0.1"
		return
	}

	var callback js.Func
	callback = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		defer callback.Release()
		
		if len(args) > 0 && !args[0].IsNull() && !args[0].IsUndefined() {
			result := args[0]
			if !result.Get("ip").IsUndefined() {
				ip := result.Get("ip").String()
				logP2P("info", "IP obtenida del servidor: %s", ip)
				done <- ip
				return nil
			}
		}
		
		if attempt < maxRetries {
			logP2P("info", "Reintentando obtener IP, intento: %d", attempt+1)
			time.Sleep(retryDelay)
			go tryGetIP(done, attempt+1, maxRetries, retryDelay)
		} else {
			logP2P("warn", "No se pudo obtener IP después de %d intentos", maxRetries)
			done <- "127.0.0.1"
		}
		return nil
	})

	var errorCallback js.Func
	errorCallback = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		defer errorCallback.Release()
		
		if attempt < maxRetries {
			js.Global().Get("console").Call("log", "Error obteniendo IP, reintentando:", attempt+1)
				time.Sleep(retryDelay)
				go tryGetIP(done, attempt+1, maxRetries, retryDelay)
		} else {
			done <- "127.0.0.1"
		}
		return nil
	})

	// Intentar obtener la información del servidor
	promise := electronObj.Call("getServerInfo")
	if !promise.IsUndefined() && !promise.IsNull() {
		promise.Call("then", callback).Call("catch", errorCallback)
	} else {
		done <- "127.0.0.1"
	}
}

func getServerIP() string {
	// Primero intentar obtener la IP local directamente
	ip := getOutboundIP()
	if ip != "127.0.0.1" {
		logP2P("info", "IP obtenida localmente: %s", ip)
		return ip
	}

	// Si electron está disponible, intentar obtener la IP del servidor
	electronObj := js.Global().Get("electron")
	if !electronObj.IsUndefined() && !electronObj.IsNull() {
		// Crear un canal para recibir el resultado
		done := make(chan string)
		maxRetries := 5
		retryDelay := time.Second * 2

		// Iniciar el primer intento
		go tryGetIP(done, 0, maxRetries, retryDelay)

		// Esperar el resultado con timeout
		select {
		case ip := <-done:
			return ip
		case <-time.After(time.Second * 15):
			logP2P("warn", "Timeout obteniendo IP del servidor")
			return "127.0.0.1"
		}
	}

	logP2P("warn", "Electron no disponible, usando IP local")
	return "127.0.0.1"
}

// Función para encontrar un puerto disponible en un rango
func findAvailablePort(startPort, endPort int) int {
	// Intentar primero con el puerto inicial
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", startPort))
	if err == nil {
		listener.Close()
		return startPort
	}

	// Si el puerto inicial no está disponible, buscar otro
	for port := startPort; port <= endPort; port++ {
		listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
		if err == nil {
			listener.Close()
			return port
		}
	}
	return -1 // No se encontró puerto disponible
}

func initP2P() error {
	logP2P("info", "Iniciando sistema P2P...")
	
	var err error
	ctx = context.Background()

	// Obtener la IP del servidor
	localIP := getServerIP()
	logP2P("info", "IP del servidor: %s", localIP)

	// Usar puertos fijos para mejor predictibilidad
	wsPort := 9100  // Puerto WebSocket fijo
	tcpPort := 9101 // Puerto TCP fijo

	if wsPort == -1 || tcpPort == -1 {
		logP2P("error", "No se encontraron puertos disponibles")
		return fmt.Errorf("no se encontraron puertos disponibles")
	}

	logP2P("info", "Puertos asignados - WS: %d, TCP: %d", wsPort, tcpPort)

	// Lista de direcciones para escuchar
	listenAddrs := []string{
		fmt.Sprintf("/ip4/%s/tcp/%d", localIP, tcpPort),
		fmt.Sprintf("/ip4/%s/tcp/%d/ws", localIP, wsPort),
	}

	logP2P("info", "Configurando direcciones de escucha:")
	for _, addr := range listenAddrs {
		logP2P("info", "  • %s", addr)
	}

	// Verificar que los puertos estén realmente disponibles antes de continuar
	for _, port := range []int{wsPort, tcpPort} {
		addr := fmt.Sprintf("%s:%d", localIP, port)
		l, err := net.Listen("tcp", addr)
		if err != nil {
			logP2P("error", "Puerto %d no disponible: %v", port, err)
			return fmt.Errorf("puerto %d no disponible", port)
		}
		l.Close()
	}

	// Configuración del nodo P2P
	node, err = libp2p.New(
		libp2p.ListenAddrStrings(listenAddrs...),
		libp2p.DefaultSecurity,
		libp2p.DefaultMuxers,
		libp2p.Transport(websocket.New),
		libp2p.DisableRelay(),
		libp2p.EnableNATService(),
		libp2p.NATPortMap(),
		libp2p.EnableHolePunching(),
	)
	if err != nil {
		logP2P("error", "Error creando nodo P2P: %v", err)
		return fmt.Errorf("error creando nodo p2p: %v", err)
	}

	// Verificar que el nodo está escuchando en las direcciones correctas
	actualAddrs := node.Addrs()
	logP2P("info", "Direcciones activas del nodo:")
	for _, addr := range actualAddrs {
		logP2P("info", "  • %s", addr.String())
	}

	node.SetStreamHandler("/warcraft/lan/1.0.0", handleStream)
	logP2P("info", "Nodo P2P inicializado correctamente")
	return nil
}

func handleStream(stream network.Stream) {
	defer stream.Close()

	remotePeer := stream.Conn().RemotePeer()
	remoteAddr := stream.Conn().RemoteMultiaddr()
	
	logP2P("info", "Nueva conexión entrante:")
	logP2P("info", "  • Peer ID: %s", remotePeer.String())
	logP2P("info", "  • Dirección: %s", remoteAddr.String())
	logP2P("info", "  • Protocolo: %s", stream.Protocol())

	// Leer el mensaje
	buf := make([]byte, 1024)
	n, err := stream.Read(buf)
	if err != nil && err != io.EOF {
		logP2P("error", "Error leyendo stream: %v", err)
		return
	}

	message := string(buf[:n])
	logP2P("info", "Mensaje recibido: %s", message)

	// Emitir evento para la UI
	js.Global().Get("document").Call("dispatchEvent",
		js.ValueOf(map[string]interface{}{
			"type":    "gameMessage",
			"detail":  message,
			"bubbles": true,
		}),
	)
}

func getPeerInfo() js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		if node == nil {
			return "No P2P node available"
		}

		addrs := make([]string, 0)
		for _, addr := range node.Addrs() {
			fullAddr := fmt.Sprintf("%s/p2p/%s", addr.String(), node.ID().String())
			addrs = append(addrs, fullAddr)
		}

		peerInfo := PeerInfo{
			ID:        node.ID().String(),
			Addresses: addrs,
			Name:      peerName,
		}

		// Agregar información de peers conectados
		connectedPeers := make([]string, 0)
		for _, p := range node.Network().Peers() {
			connectedPeers = append(connectedPeers, p.String())
		}
		
		peerInfoMap := map[string]interface{}{
			"id":        peerInfo.ID,
			"addresses": peerInfo.Addresses,
			"name":      peerInfo.Name,
			"connected": connectedPeers,
		}

		jsonBytes, err := json.Marshal(peerInfoMap)
		if err != nil {
			return err.Error()
		}

		return string(jsonBytes)
	})
}

func connectToPeer() js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		if len(args) < 1 {
			logP2P("error", "Se requiere la dirección del peer")
			return "Se requiere la dirección del peer"
		}

		peerAddr := args[0].String()
		logP2P("info", "Analizando dirección del peer: %s", peerAddr)

		ma, err := multiaddr.NewMultiaddr(peerAddr)
		if err != nil {
			logP2P("error", "Error en la dirección: %v", err)
			return fmt.Sprintf("Error en la dirección del peer: %v", err)
		}

		ip, err := ma.ValueForProtocol(multiaddr.P_IP4)
		if err != nil {
			logP2P("error", "No se pudo extraer IP de la dirección: %v", err)
			return "Dirección IP no válida"
		}

		port, err := ma.ValueForProtocol(multiaddr.P_TCP)
		if err != nil {
			logP2P("error", "No se pudo extraer puerto de la dirección: %v", err)
			return "Puerto no válido"
		}

		logP2P("info", "Verificando conectividad básica con %s:%s", ip, port)
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%s", ip, port), 5*time.Second)
		if err != nil {
			logP2P("error", "No se puede establecer conexión TCP básica: %v", err)
			return fmt.Sprintf("No se puede conectar al peer: %v", err)
		}
		conn.Close()

		peerinfo, err := peer.AddrInfoFromP2pAddr(ma)
		if err != nil {
			logP2P("error", "Error obteniendo info del peer: %v", err)
			return fmt.Sprintf("Error obteniendo info del peer: %v", err)
		}

		logP2P("info", "Información del peer objetivo:")
		logP2P("info", "  • ID: %s", peerinfo.ID.String())
		logP2P("info", "  • Direcciones: %v", peerinfo.Addrs)

		if peerinfo.ID == node.ID() {
			logP2P("error", "Intento de conexión a sí mismo detectado")
			return "Error: No puedes conectarte a tu propia dirección"
		}

		// Crear contexto con timeout
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Intentar la conexión con retry simple
		maxRetries := 3
		for i := 0; i < maxRetries; i++ {
			logP2P("info", "Intento de conexión %d/%d al peer %s", i+1, maxRetries, peerinfo.ID.String())

			err := node.Connect(ctx, *peerinfo)
			if err == nil {
				logP2P("info", "✅ Conexión establecida exitosamente con %s", peerinfo.ID.String())
				return "Conectado exitosamente"
			}

			logP2P("warn", "Intento %d fallido: %v", i+1, err)

			if i < maxRetries-1 {
				time.Sleep(2 * time.Second)
			}
		}

		errMsg := fmt.Sprintf("No se pudo conectar al peer después de %d intentos", maxRetries)
		logP2P("error", errMsg)
		return errMsg
	})
}

func broadcastGame() js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		if len(args) < 1 {
			return "Se requiere información del juego"
		}

		gameInfo := args[0].String()
		for _, p := range node.Network().Peers() {
			stream, err := node.NewStream(ctx, p, "/warcraft/1.0.0")
			if err != nil {
				js.Global().Get("console").Call("error", "Error creando stream:", err.Error())
				continue
			}
			_, err = stream.Write([]byte(gameInfo))
			if err != nil {
				js.Global().Get("console").Call("error", "Error enviando mensaje:", err.Error())
				stream.Close()
				continue
			}
			stream.Close()
		}

		return nil
	})
}

// Nueva función auxiliar para imprimir información del nodo
func printNodeInfo() {
	multiaddrs := node.Addrs()
	for _, addr := range multiaddrs {
		fullAddr := fmt.Sprintf("%s/p2p/%s", addr.String(), node.ID().String())
		js.Global().Get("console").Call("log", "Dirección completa:", fullAddr)
		
		// Analizar componentes de la dirección
		components := addr.Protocols()
		for _, comp := range components {
			js.Global().Get("console").Call("log", "Protocolo:", 
				fmt.Sprintf("- %s (%d)", comp.Name, comp.Code))
		}
	}
}

// Añadir función para loggear eventos de red
func logNetworkEvent(eventType string, peerID peer.ID, addr multiaddr.Multiaddr) {
	js.Global().Get("console").Call("group", fmt.Sprintf("🌐 Evento de red: %s", eventType))
	js.Global().Get("console").Call("log", " Peer ID:", peerID.String())
	js.Global().Get("console").Call("log", "🔹 Dirección:", addr.String())
	js.Global().Get("console").Call("log", "🔹 Timestamp:", time.Now().Format(time.RFC3339))
	js.Global().Get("console").Call("groupEnd")
}

// Agregar nueva función para verificar puerto
func checkPortAvailable(ip string, port int) error {
	// Intentar escuchar en el puerto específico
	addr := fmt.Sprintf("%s:%d", ip, port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	defer listener.Close()

	// Verificar que podemos conectarnos al puerto
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return fmt.Errorf("no se puede conectar al puerto: %v", err)
	}
	conn.Close()

	return nil
} 