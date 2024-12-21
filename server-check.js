async function checkServer() {
    const maxAttempts = 10;
    const delay = 1000; // 1 segundo

    for (let i = 0; i < maxAttempts; i++) {
        try {
            const response = await fetch('http://localhost:8080/server-info');
            if (response.ok) {
                console.log('Servidor disponible');
                return true;
            }
        } catch (err) {
            console.log(`Intento ${i + 1}: Servidor no disponible`);
        }
        await new Promise(resolve => setTimeout(resolve, delay));
    }
    console.error('No se pudo conectar al servidor');
    return false;
}

// Verificar el servidor antes de inicializar WASM
checkServer().then(available => {
    if (available) {
        // Inicializar WASM
        const go = new Go();
        WebAssembly.instantiateStreaming(fetch("wasm/game.wasm"), go.importObject)
            .then((result) => {
                go.run(result.instance);
            })
            .catch(err => console.error('Error cargando WASM:', err));
    } else {
        document.body.innerHTML = '<h1>Error: No se pudo conectar al servidor</h1>';
    }
}); 