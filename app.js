// Inicializar WASM
const go = new Go();
WebAssembly.instantiateStreaming(fetch("wasm/game.wasm"), go.importObject)
    .then((result) => {
        go.run(result.instance);
    })
    .catch(err => console.error('Error cargando WASM:', err));

// Funciones de la aplicación
async function selectAndUpdatePath() {
    try {
        const path = await window.electron.selectFile();
        if (path) {
            updateGamePath(path);
        }
    } catch (err) {
        console.error('Error seleccionando archivo:', err);
    }
}

async function showPeerInfo() {
    try {
        const peerInfoStr = getPeerInfo();
        const peerInfo = JSON.parse(peerInfoStr);
        const display = document.getElementById('peerInfoDisplay');
        // ... resto del código ...
    } catch (err) {
        console.error('Error mostrando información P2P:', err);
        document.getElementById('peerInfoDisplay').innerHTML =
            `<p class="error">Error: ${err.message}</p>`;
    }
}

// Agregar event listeners cuando el DOM esté listo
document.addEventListener('DOMContentLoaded', () => {
    // Botones principales
    document.getElementById('selectFileBtn').addEventListener('click', selectAndUpdatePath);
    document.getElementById('launchButton').addEventListener('click', () => launchGame());
    document.getElementById('showPeerInfoBtn').addEventListener('click', showPeerInfo);
    document.getElementById('connectPeerBtn').addEventListener('click', connectWithPeer);

    // Event listener para mensajes P2P
    document.addEventListener('gameMessage', function (e) {
        console.log('Mensaje de juego recibido:', e.detail);
        const messagesList = document.getElementById('messagesList');
        if (messagesList) {
            const li = document.createElement('li');
            li.textContent = e.detail;
            messagesList.appendChild(li);
        }
    });
}); 