package main

import (
	"path/filepath"
	"strings"
	"syscall/js"
)

var (
	gamePath string
	callbacks []js.Func
)

func updateGamePath(this js.Value, args []js.Value) interface{} {
	if len(args) == 0 {
		js.Global().Get("console").Call("error", "Error: No se recibió ninguna ruta")
		return nil
	}

	gamePath = args[0].String()
	js.Global().Get("console").Call("log", "Ruta recibida:", gamePath)

	if !isValidExecutable(gamePath) {
		js.Global().Get("console").Call("error", "Error: El archivo no es un ejecutable válido")
		return nil
	}
	
	js.Global().Get("console").Call("log", "Archivo válido, habilitando botón")
	js.Global().Get("document").Call("getElementById", "launchButton").Set("disabled", false)
	return nil
}

func isValidExecutable(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".exe"
}

func launchGame() {
	if gamePath == "" {
		js.Global().Get("console").Call("error", "Error: No se ha seleccionado la ruta del juego")
		return
	}

	js.Global().Get("console").Call("log", "Intentando iniciar el juego en:", gamePath)

	// Usar el API de Electron para iniciar el sandbox
	promise := js.Global().Get("electron").Call("launchInSandbox", gamePath)
	
	promise.Call("then", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		if args[0].Bool() {
			js.Global().Get("console").Call("log", "Juego iniciado correctamente en Windows Sandbox")
		} else {
			js.Global().Get("console").Call("error", "Error al iniciar el sandbox")
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
	// Crear un canal que nunca se cerrará
	c := make(chan struct{})
	
	// Registrar todas las funciones de callback
	registerCallbacks()
	
	// Inicializar P2P
	if err := initP2P(); err != nil {
		js.Global().Get("console").Call("error", "Error iniciando P2P:", err.Error())
	} else {
		js.Global().Get("console").Call("log", "P2P inicializado correctamente")
	}
	
	js.Global().Get("console").Call("log", "WASM inicializado correctamente")

	// Asegurarnos de limpiar los callbacks cuando el programa termine
	defer func() {
		for _, cb := range callbacks {
			cb.Release()
		}
	}()
	
	// Esperar indefinidamente
	<-c
} 