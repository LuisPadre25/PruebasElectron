package main

import (
    "bufio"
    "encoding/json"
    "log"
    "net/url"
    "os"
    "sync"

    "github.com/gorilla/websocket"
    "github.com/pion/webrtc/v3"
)

// SignalMessage representa la estructura de intercambio por WebSocket
type SignalMessage struct {
    Type    string `json:"type"`
    Payload string `json:"payload"`
}

func main() {
    // 1) Dirección del servidor de señalización
    serverURL := "ws://149.28.106.4:8080/ws"
    if len(os.Args) > 1 {
        serverURL = os.Args[1]
    }
    log.Println("Conectando al servidor de señalización en:", serverURL)

    // Conectar WebSocket
    u, err := url.Parse(serverURL)
    if err != nil {
        log.Fatal(err)
    }

    wsConn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
    if err != nil {
        log.Fatal("Error al conectar con WebSocket:", err)
    }
    defer wsConn.Close()

    // 2) Configuración ICE (STUN/TURN)
    config := webrtc.Configuration{
        ICEServers: []webrtc.ICEServer{
            {
                URLs: []string{
                    "stun:149.28.106.4:3478",
                    "turn:149.28.106.4:3478?transport=udp",
                },
                Username:   "gameuser",
                Credential: "gamepass",
            },
        },
    }

    // 3) Crear PeerConnection
    peerConnection, err := webrtc.NewPeerConnection(config)
    if err != nil {
        log.Fatal(err)
    }
    defer peerConnection.Close()

    // 4) Crear DataChannel para chatear
    dataChannel, err := peerConnection.CreateDataChannel("chat", nil)
    if err != nil {
        log.Fatal(err)
    }

    // Evento: DataChannel abierta
    dataChannel.OnOpen(func() {
        log.Println("[DataChannel abierta] Ya puedes escribir mensajes en la consola...")
    })

    // Evento: Mensaje recibido
    dataChannel.OnMessage(func(msg webrtc.DataChannelMessage) {
        log.Printf("[Otro Peer dice]: %s\n", string(msg.Data))
    })

    // 5) Cuando generemos ICE candidates, enviarlos al otro peer
    peerConnection.OnICECandidate(func(candidate *webrtc.ICECandidate) {
        if candidate != nil {
            candidateJSON, _ := json.Marshal(candidate.ToJSON())
            sendSignal(wsConn, SignalMessage{
                Type:    "candidate",
                Payload: string(candidateJSON),
            })
        }
    })

    // 6) Leer señales entrantes (offer, answer, candidates) por WebSocket
    var wg sync.WaitGroup
    wg.Add(1)

    go func() {
        defer wg.Done()
        for {
            _, msgBytes, err := wsConn.ReadMessage()
            if err != nil {
                log.Println("WebSocket cerrado:", err)
                return
            }

            var msg SignalMessage
            if err := json.Unmarshal(msgBytes, &msg); err != nil {
                log.Println("Error al parsear JSON:", err)
                continue
            }

            switch msg.Type {
            case "ready":
                // Crear offer
                log.Println("Servidor dice 'ready'. Creando offer...")
                offer, err := peerConnection.CreateOffer(nil)
                if err != nil {
                    log.Println("Error creando offer:", err)
                    return
                }
                if err := peerConnection.SetLocalDescription(offer); err != nil {
                    log.Println("Error SetLocalDescription:", err)
                    return
                }

                offerBytes, _ := json.Marshal(offer)
                sendSignal(wsConn, SignalMessage{
                    Type:    "offer",
                    Payload: string(offerBytes),
                })

            case "offer":
                // Recibir la oferta
                var offer webrtc.SessionDescription
                _ = json.Unmarshal([]byte(msg.Payload), &offer)
                log.Println("Oferta recibida. Creando answer...")

                if err := peerConnection.SetRemoteDescription(offer); err != nil {
                    log.Println("Error SetRemoteDescription(offer):", err)
                    return
                }

                answer, err := peerConnection.CreateAnswer(nil)
                if err != nil {
                    log.Println("Error creando answer:", err)
                    return
                }

                if err := peerConnection.SetLocalDescription(answer); err != nil {
                    log.Println("Error SetLocalDescription(answer):", err)
                    return
                }

                answerBytes, _ := json.Marshal(answer)
                sendSignal(wsConn, SignalMessage{
                    Type:    "answer",
                    Payload: string(answerBytes),
                })

            case "answer":
                // Recibir la respuesta
                var answer webrtc.SessionDescription
                _ = json.Unmarshal([]byte(msg.Payload), &answer)
                log.Println("Respuesta recibida. SetRemoteDescription...")

                if err := peerConnection.SetRemoteDescription(answer); err != nil {
                    log.Println("Error SetRemoteDescription(answer):", err)
                }

            case "candidate":
                // Recibir un ICE candidate
                var candidate webrtc.ICECandidateInit
                _ = json.Unmarshal([]byte(msg.Payload), &candidate)
                peerConnection.AddICECandidate(candidate)
            }
        }
    }()

    // 7) Leer del stdin y enviar por el DataChannel
    go func() {
        scanner := bufio.NewScanner(os.Stdin)
        for scanner.Scan() {
            text := scanner.Text()
            if dataChannel.ReadyState() == webrtc.DataChannelStateOpen {
                if err := dataChannel.SendText(text); err != nil {
                    log.Println("Error enviando mensaje:", err)
                }
            } else {
                log.Println("[DataChannel aún no abierta]")
            }
        }
    }()

    // Esperar a que la goroutine de lectura (WebSocket) termine
    wg.Wait()
}

// sendSignal envía un mensaje de señalización vía WebSocket
func sendSignal(wsConn *websocket.Conn, msg SignalMessage) {
    msgBytes, _ := json.Marshal(msg)
    wsConn.WriteMessage(websocket.TextMessage, msgBytes)
}
