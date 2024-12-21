package main

import (
	"net"
	"fmt"
	"time"
)

type NATTraversal struct {
	stunServers []string
	localPort   int
}

func NewNATTraversal() *NATTraversal {
	return &NATTraversal{
		stunServers: []string{
			"stun.l.google.com:19302",
			"stun1.l.google.com:19302",
			"stun2.l.google.com:19302",
		},
		localPort: 0, // Puerto aleatorio
	}
}

// Función para establecer conexión P2P
func (n *NATTraversal) Connect(peerAddr string) (net.Conn, error) {
	// 1. Crear socket UDP local
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{
		IP:   net.IPv4zero,
		Port: n.localPort,
	})
	if err != nil {
		return nil, err
	}

	// 2. Obtener IP pública usando múltiples servidores STUN
	publicIP, err := n.getPublicIP(conn)
	if err != nil {
		return nil, err
	}

	// 3. Intentar conexión directa usando la IP pública
	peerUDPAddr, err := net.ResolveUDPAddr("udp4", peerAddr)
	if err != nil {
		return nil, fmt.Errorf("error resolviendo dirección peer: %v", err)
	}

	fmt.Printf("Intentando conexión directa a %s desde %s\n", peerAddr, publicIP)
	
	// 4. Enviar mensaje de prueba
	_, err = conn.WriteToUDP([]byte("HELLO"), peerUDPAddr)
	if err != nil {
		return nil, fmt.Errorf("error enviando mensaje inicial: %v", err)
	}

	// 5. Esperar respuesta
	buffer := make([]byte, 1024)
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, remoteAddr, err := conn.ReadFromUDP(buffer)
	if err != nil {
		return nil, fmt.Errorf("error esperando respuesta: %v", err)
	}

	fmt.Printf("Conexión P2P establecida con %v\n", remoteAddr)
	return conn, nil
}

func (n *NATTraversal) getPublicIP(conn *net.UDPConn) (string, error) {
	for _, server := range n.stunServers {
		// Resolver dirección del servidor STUN
		stunAddr, err := net.ResolveUDPAddr("udp4", server)
		if err != nil {
			continue
		}

		// Enviar solicitud STUN
		_, err = conn.WriteToUDP([]byte("STUN"), stunAddr)
		if err != nil {
			continue
		}

		// Esperar respuesta
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		buffer := make([]byte, 1024)
		_, addr, err := conn.ReadFromUDP(buffer)
		if err != nil {
			continue
		}

		// La dirección desde la que recibimos la respuesta es nuestra IP pública
		return addr.IP.String(), nil
	}

	return "", fmt.Errorf("no se pudo obtener IP pública de ningún servidor STUN")
} 