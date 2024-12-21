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

	// Buscar puertos disponibles
	wsPort := findAvailablePort(9100, 9200)
	tcpPort := findAvailablePort(9201, 9300)

	if wsPort == -1 || tcpPort == -1 {
		return fmt.Errorf("no se encontraron puertos disponibles")
	}

	// Lista de direcciones para escuchar
	listenAddrs := []string{
		fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", tcpPort),
		fmt.Sprintf("/ip4/0.0.0.0/tcp/%d/ws", wsPort),
		fmt.Sprintf("/ip4/%s/tcp/%d", localIP, tcpPort),
		fmt.Sprintf("/ip4/%s/tcp/%d/ws", localIP, wsPort),
	}

	// Configuración mejorada del nodo P2P
	node, err = libp2p.New(
		libp2p.ListenAddrStrings(listenAddrs...),
		libp2p.DefaultSecurity,
		libp2p.DefaultMuxers,
		libp2p.Transport(websocket.New),
		libp2p.EnableRelay(),
		libp2p.EnableNATService(),
		libp2p.NATPortMap(),
		libp2p.EnableAutoRelay(),
		libp2p.EnableHolePunching(),
	)
	if err != nil {
		return fmt.Errorf("error creando nodo p2p: %v", err)
	}

	// Imprimir información detallada del nodo
	printNodeInfo()

	// Configurar el manejador de streams
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
		js.Global().Get("console").Call("log", "Intentando conectar a:", peerAddr)

		ma, err := multiaddr.NewMultiaddr(peerAddr)
		if err != nil {
			errMsg := fmt.Sprintf("Error en la dirección del peer: %v", err)
			js.Global().Get("console").Call("error", errMsg)
			return errMsg
		}

		peerinfo, err := peer.AddrInfoFromP2pAddr(ma)
		if err != nil {
			errMsg := fmt.Sprintf("Error obteniendo info del peer: %v", err)
			js.Global().Get("console").Call("error", errMsg)
			return errMsg
		}

		// Verificar que no estemos intentando conectarnos a nosotros mismos
		if peerinfo.ID == node.ID() {
			return "Error: No puedes conectarte a tu propia dirección"
		}

		// Crear un contexto con timeout más largo
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		// Mejorar el sistema de retry con backoff exponencial
		maxRetries := 5
		baseDelay := time.Second
		for i := 0; i < maxRetries; i++ {
			// Calcular delay exponencial
			delay := baseDelay * time.Duration(1<<uint(i))
			
			js.Global().Get("console").Call("log", 
				fmt.Sprintf("Intento de conexión %d/%d", i+1, maxRetries))

			// Intentar la conexión
			err := node.Connect(ctx, *peerinfo)
			if err == nil {
				// Verificar si la conexión está realmente establecida
				if node.Network().Connectedness(peerinfo.ID) == network.Connected {
					js.Global().Get("console").Call("log", 
						"Conexión establecida exitosamente con:", peerinfo.ID.String())
					return "Conectado exitosamente"
				}
			}

			// Loguear el error específico
			js.Global().Get("console").Call("warn", 
				fmt.Sprintf("Intento %d fallido: %v", i+1, err))

			// Si no es el último intento, esperar antes del siguiente
			if i < maxRetries-1 {
				js.Global().Get("console").Call("log", 
					fmt.Sprintf("Esperando %v antes del siguiente intento...", delay))
				time.Sleep(delay)
			}
		}

		errMsg := "Error: No se pudo establecer la conexión después de varios intentos. " +
			"Verifica que la dirección sea correcta y que el peer esté en línea."
		js.Global().Get("console").Call("error", errMsg)
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