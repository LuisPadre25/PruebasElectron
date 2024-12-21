package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strings"
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

func getOutboundIP() string {
	// Intentar obtener todas las interfaces de red
	interfaces, err := net.Interfaces()
	if err != nil {
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

			js.Global().Get("console").Call("log", "Interfaz encontrada:", iface.Name, "IP:", ip.String())
			return ip.String()
		}
	}

	// Si no se encuentra ninguna IP válida, intentar el método de conexión UDP
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
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
					done <- ip
					return nil
			}
		}
		
		if attempt < maxRetries {
			js.Global().Get("console").Call("log", "Reintentando obtener IP, intento:", attempt+1)
			time.Sleep(retryDelay)
			go tryGetIP(done, attempt+1, maxRetries, retryDelay)
		} else {
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
		js.Global().Get("console").Call("log", "IP obtenida localmente:", ip)
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
			js.Global().Get("console").Call("warn", "Timeout obteniendo IP del servidor")
			return "127.0.0.1"
		}
	}

	js.Global().Get("console").Call("warn", "Electron no disponible, usando IP local")
	return "127.0.0.1"
}

// Función para encontrar un puerto disponible en un rango
func findAvailablePort(startPort, endPort int) int {
	for port := startPort; port <= endPort; port++ {
		// Intentar escuchar en el puerto
		listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
		if err == nil {
			listener.Close()
			return port
		}
	}
	return -1 // No se encontró puerto disponible
}

func initP2P() error {
	var err error
	ctx = context.Background()

	// Obtener la IP del servidor
	localIP := getServerIP()
	js.Global().Get("console").Call("log", "IP del servidor:", localIP)

	// Buscar puertos disponibles en rangos menos comunes
	// Evitamos puertos comunes como 80, 443, 3000-3999, 8000-8999
	wsPort := findAvailablePort(9100, 9200)  // Rango para WebSocket
	tcpPort := findAvailablePort(9201, 9300) // Rango para TCP

	if wsPort == -1 || tcpPort == -1 {
		return fmt.Errorf("no se encontraron puertos disponibles")
	}

	js.Global().Get("console").Call("log", "Usando puertos - WS:", wsPort, "TCP:", tcpPort)

	// Lista de direcciones para escuchar
	listenAddrs := []string{
		// Escuchar en todas las interfaces
		fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", tcpPort),
		fmt.Sprintf("/ip4/0.0.0.0/tcp/%d/ws", wsPort),
		// También escuchar específicamente en la IP local
		fmt.Sprintf("/ip4/%s/tcp/%d", localIP, tcpPort),
		fmt.Sprintf("/ip4/%s/tcp/%d/ws", localIP, wsPort),
	}

	// Crear un nuevo nodo P2P con configuración específica
	node, err = libp2p.New(
		// Escuchar en las direcciones configuradas
		libp2p.ListenAddrStrings(listenAddrs...),
		// Configuración de seguridad
		libp2p.DefaultSecurity,
		// Configuración de multiplexing
		libp2p.DefaultMuxers,
		// Transportes
		libp2p.Transport(websocket.New),
		// Habilitar discovery
		libp2p.EnableRelay(),
		// Habilitar NAT traversal
		libp2p.EnableNATService(),
		libp2p.NATPortMap(),
	)
	if err != nil {
		return fmt.Errorf("error creando nodo p2p: %v", err)
	}

	// Imprimir información del nodo para depuración
	multiaddrs := node.Addrs()
	addrStrings := make([]string, 0, len(multiaddrs))
	for _, addr := range multiaddrs {
		addrStrings = append(addrStrings, addr.String())
		// Crear una dirección multiaddr completa para cada endpoint
		fullAddr := fmt.Sprintf("%s/p2p/%s", addr.String(), node.ID().String())
		js.Global().Get("console").Call("log", "Dirección completa:", fullAddr)
	}

	js.Global().Get("console").Call("log", "IP Local:", localIP)
	js.Global().Get("console").Call("log", "Direcciones del nodo:", strings.Join(addrStrings, ", "))
	js.Global().Get("console").Call("log", "ID del nodo:", node.ID().String())

	// Manejar mensajes entrantes
	node.SetStreamHandler("/warcraft/1.0.0", handleStream)

	return nil
}

func handleStream(stream network.Stream) {
	defer stream.Close()

	// Leer el mensaje
	buf := make([]byte, 1024)
	n, err := stream.Read(buf)
	if err != nil && err != io.EOF {
		js.Global().Get("console").Call("error", "Error leyendo stream:", err.Error())
		return
	}

	// Procesar el mensaje
	message := string(buf[:n])
	js.Global().Get("console").Call("log", "Mensaje recibido:", message)

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
			return "Se requiere la dirección del peer"
		}

		peerAddr := args[0].String()
		ma, err := multiaddr.NewMultiaddr(peerAddr)
		if err != nil {
			return fmt.Sprintf("Error en la dirección del peer: %v", err)
		}

		peerinfo, err := peer.AddrInfoFromP2pAddr(ma)
		if err != nil {
			return fmt.Sprintf("Error obteniendo info del peer: %v", err)
		}

		// Verificar que no estemos intentando conectarnos a nosotros mismos
		if peerinfo.ID == node.ID() {
			return "Error: No puedes conectarte a tu propia dirección"
		}

		// Verificar si ya estamos conectados
		for _, p := range node.Network().Peers() {
			if p == peerinfo.ID {
				return "Ya estás conectado a este peer"
			}
		}

		if err := node.Connect(ctx, *peerinfo); err != nil {
			return fmt.Sprintf("Error conectando al peer: %v", err)
		}

		return "Conectado exitosamente"
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