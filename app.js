// Inicializar WASM
const go = new Go();
WebAssembly.instantiateStreaming(fetch("wasm/game.wasm"), go.importObject)
    .then((result) => {
        console.log('🚀 WASM inicializado correctamente');
        go.run(result.instance);
    })
    .catch(err => console.error('❌ Error cargando WASM:', err));

// Función de debug
function debugLog(category, message, data = null) {
    const timestamp = new Date().toISOString();
    const prefix = `[${category}] ${timestamp}:`;
    
    if (data) {
        console.log(prefix, message, data);
    } else {
        console.log(prefix, message);
    }

    // Opcional: Mostrar en la UI
    const debugDiv = document.getElementById('debugLog');
    if (debugDiv) {
        const logEntry = document.createElement('div');
        logEntry.className = `debug-entry ${category.toLowerCase()}`;
        logEntry.textContent = `${prefix} ${message} ${data ? JSON.stringify(data) : ''}`;
        debugDiv.appendChild(logEntry);
        debugDiv.scrollTop = debugDiv.scrollHeight;
    }
}

// Funciones de la aplicación
async function selectAndUpdatePath() {
    try {
        debugLog('FILE', 'Iniciando selección de archivo...');
        const path = await window.electron.selectFile();
        if (path) {
            debugLog('FILE', 'Archivo seleccionado:', path);
            updateGamePath(path);
        }
    } catch (err) {
        debugLog('ERROR', 'Error seleccionando archivo:', err);
    }
}

async function showPeerInfo() {
    try {
        debugLog('P2P', 'Obteniendo información del peer...');
        const peerInfoStr = getPeerInfo();
        const peerInfo = JSON.parse(peerInfoStr);
        debugLog('P2P', 'Información del peer obtenida:', peerInfo);

        const display = document.getElementById('peerInfoDisplay');
        if (display) {
            let html = '<h3>Información del Peer</h3>';
            html += `<p><strong>ID:</strong> ${peerInfo.id}</p>`;
            html += '<p><strong>Direcciones:</strong></p><ul>';
            peerInfo.addresses.forEach(addr => {
                html += `<li>${addr}</li>`;
            });
            html += '</ul>';
            
            if (peerInfo.connected && peerInfo.connected.length > 0) {
                html += '<p><strong>Peers Conectados:</strong></p><ul>';
                peerInfo.connected.forEach(peer => {
                    html += `<li>${peer}</li>`;
                });
                html += '</ul>';
            }
            
            display.innerHTML = html;
        }
    } catch (err) {
        debugLog('ERROR', 'Error mostrando información P2P:', err);
        const display = document.getElementById('peerInfoDisplay');
        if (display) {
            display.innerHTML = `<p class="error">Error: ${err.message}</p>`;
        }
    }
}

async function connectWithPeer() {
    const peerAddr = document.getElementById('peerAddr').value;
    if (!peerAddr) {
        debugLog('ERROR', 'Dirección del peer no proporcionada');
        return;
    }

    debugLog('P2P', 'Intentando conectar con peer:', peerAddr);
    try {
        const result = connectToPeer(peerAddr);
        debugLog('P2P', 'Resultado de la conexión:', result);
    } catch (err) {
        debugLog('ERROR', 'Error conectando con peer:', err);
    }
}

// Agregar event listeners cuando el DOM esté listo
document.addEventListener('DOMContentLoaded', () => {
    debugLog('INIT', 'Inicializando aplicación...');

    // Botones principales
    document.getElementById('selectFileBtn')?.addEventListener('click', selectAndUpdatePath);
    document.getElementById('launchButton')?.addEventListener('click', () => {
        debugLog('GAME', 'Iniciando juego...');
        launchGame();
    });
    document.getElementById('showPeerInfoBtn')?.addEventListener('click', showPeerInfo);
    document.getElementById('connectPeerBtn')?.addEventListener('click', connectWithPeer);

    // Event listener para mensajes P2P
    document.addEventListener('gameMessage', function (e) {
        debugLog('P2P', 'Mensaje de juego recibido:', e.detail);
        const messagesList = document.getElementById('messagesList');
        if (messagesList) {
            const li = document.createElement('li');
            li.textContent = e.detail;
            messagesList.appendChild(li);
        }
    });

    debugLog('INIT', 'Aplicación inicializada correctamente');
});

// Añadir estilos para el debug log
const style = document.createElement('style');
style.textContent = `
    #debugLog {
        position: fixed;
        bottom: 0;
        right: 0;
        width: 400px;
        height: 200px;
        background: rgba(0, 0, 0, 0.8);
        color: #fff;
        font-family: monospace;
        font-size: 12px;
        padding: 10px;
        overflow-y: auto;
        z-index: 9999;
    }
    .debug-entry {
        margin: 2px 0;
        padding: 2px 5px;
        border-left: 3px solid #666;
    }
    .debug-entry.p2p { border-color: #4CAF50; }
    .debug-entry.error { border-color: #f44336; }
    .debug-entry.init { border-color: #2196F3; }
    .debug-entry.file { border-color: #FF9800; }
    .debug-entry.game { border-color: #9C27B0; }
`;
document.head.appendChild(style);

// Crear el div para el debug log
document.addEventListener('DOMContentLoaded', () => {
    const debugDiv = document.createElement('div');
    debugDiv.id = 'debugLog';
    document.body.appendChild(debugDiv);
}); 