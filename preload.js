const { contextBridge, ipcRenderer } = require('electron')

contextBridge.exposeInMainWorld('electron', {
    selectFile: async () => {
        try {
            const result = await ipcRenderer.invoke('select-file')
            return result
        } catch (err) {
            console.error('Error al seleccionar archivo:', err)
            return null
        }
    },
    launchInSandbox: async (gamePath) => {
        try {
            return await ipcRenderer.invoke('launch-in-sandbox', gamePath)
        } catch (err) {
            console.error('Error al iniciar sandbox:', err)
            return false
        }
    },
    getServerInfo: async () => {
        const maxRetries = 5;
        const retryDelay = 2000; // 2 segundos

        for (let i = 0; i < maxRetries; i++) {
            try {
                const response = await fetch('http://localhost:8080/server-info');
                if (!response.ok) {
                    throw new Error(`HTTP error! status: ${response.status}`);
                }
                return await response.json();
            } catch (err) {
                console.error(`Intento ${i + 1} fallido:`, err);
                if (i < maxRetries - 1) {
                    console.log(`Esperando ${retryDelay}ms antes de reintentar...`);
                    await new Promise(resolve => setTimeout(resolve, retryDelay));
                }
            }
        }
        console.warn('Todos los intentos fallaron, usando valores por defecto');
        return { ip: '127.0.0.1', port: 8080 };
    }
}) 