package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"syscall/js"
)

var (
	gamePath string
	callbacks []js.Func
)

func logMain(category string, message string, args ...interface{}) {
	console := js.Global().Get("console")
	fullMessage := fmt.Sprintf(message, args...)
	
	switch category {
	case "error":
		console.Call("error", "❌ [Main]", fullMessage)
	case "warn":
		console.Call("warn", "⚠️ [Main]", fullMessage)
	default:
		console.Call("log", "🎮 [Main]", fullMessage)
	}
}

func updateGamePath(this js.Value, args []js.Value) interface{} {
	if len(args) == 0 {
		logMain("error", "No se recibió ninguna ruta")
		return nil
	}

	gamePath = args[0].String()
	logMain("info", "Ruta recibida: %s", gamePath)

	if !isValidExecutable(gamePath) {
		logMain("error", "El archivo no es un ejecutable válido")
		return nil
	}
	
	logMain("info", "Archivo válido, habilitando botón")
	js.Global().Get("document").Call("getElementById", "launchButton").Set("disabled", false)
	return nil
}

func isValidExecutable(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".exe"
}

func launchGame() {
	if gamePath == "" {
		logMain("error", "No se ha seleccionado la ruta del juego")
		return
	}

	logMain("info", "Intentando iniciar el juego en: %s", gamePath)

	// Usar el API de Electron para iniciar el sandbox
	promise := js.Global().Get("electron").Call("launchInSandbox", gamePath)
	
	promise.Call("then", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		if args[0].Bool() {
			logMain("info", "Juego iniciado correctamente en Windows Sandbox")
		} else {
			logMain("error", "Error al iniciar el sandbox")
		}
		return nil
	}))
}

func registerCallbacks() {
	// Crear una función de cleanup para cada callback
	cleanupCallback := func(cb js.Func) {
		callbacks = append(callbacks, cb)
	}

	// Registrar funciones básicas
	updatePathCb := js.FuncOf(updateGamePath)
	cleanupCallback(updatePathCb)
	js.Global().Set("updateGamePath", updatePathCb)

	launchGameCb := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		go launchGame()
		return nil
	})
	cleanupCallback(launchGameCb)
	js.Global().Set("launchGame", launchGameCb)

	// Registrar funciones P2P
	peerInfoCb := getPeerInfo()
	cleanupCallback(peerInfoCb)
	js.Global().Set("getPeerInfo", peerInfoCb)

	connectPeerCb := connectToPeer()
	cleanupCallback(connectPeerCb)
	js.Global().Set("connectToPeer", connectPeerCb)

	broadcastCb := broadcastGame()
	cleanupCallback(broadcastCb)
	js.Global().Set("broadcastGame", broadcastCb)
}

func main() {
	logMain("info", "Iniciando aplicación...")
	
	c := make(chan struct{})
	
	logMain("info", "Registrando callbacks...")
	registerCallbacks()
	
	logMain("info", "Iniciando sistema P2P...")
	defer func() {
		if r := recover(); r != nil {
			logMain("error", "Pánico en initP2P: %v", r)
		}
	}()

	if err := initP2P(); err != nil {
		logMain("error", "Error iniciando P2P: %v", err)
		logMain("warn", "La funcionalidad P2P estará deshabilitada")
	} else {
		logMain("info", "P2P inicializado correctamente")
		if node != nil {
			logMain("info", "Nodo P2P activo con ID: %s", node.ID().String())
			logMain("info", "Direcciones de escucha:")
			for _, addr := range node.Addrs() {
				logMain("info", "  • %s", addr.String())
			}
		} else {
			logMain("error", "Nodo P2P es nil después de la inicialización")
		}
	}
	
	logMain("info", "WASM inicializado correctamente")

	defer func() {
		logMain("info", "Limpiando callbacks...")
		for _, cb := range callbacks {
				cb.Release()
		}
	}()
	
	<-c
} 