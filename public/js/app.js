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

        let html = `
            <h3>Tu información:</h3>
            <div class="peer-info-box">
                <p><strong>ID:</strong> ${peerInfo.id}</p>
                <p><strong>Direcciones:</strong></p>
                <ul>
                    ${peerInfo.addresses.map(addr => `
                        <li>
                            <code>${addr}</code>
                            <button onclick="copyToClipboard('${addr}')" class="copy-button">
                                Copiar
                            </button>
                        </li>
                    `).join('')}
                </ul>
            </div>
            ${peerInfo.connected.length > 0 ? `
                <h3>Peers Conectados:</h3>
                <ul class="connected-peers">
                    ${peerInfo.connected.map(peer => `<li>${peer}</li>`).join('')}
                </ul>
            ` : ''}
        `;

        display.innerHTML = html;
    } catch (err) {
        console.error('Error mostrando información P2P:', err);
        document.getElementById('peerInfoDisplay').innerHTML =
            `<p class="error">Error: ${err.message}</p>`;
    }
}

function copyToClipboard(text) {
    navigator.clipboard.writeText(text)
        .then(() => {
            // Mostrar feedback visual
            const notification = document.createElement('div');
            notification.className = 'copy-notification';
            notification.textContent = '¡Copiado!';
            document.body.appendChild(notification);
            setTimeout(() => notification.remove(), 2000);
        })
        .catch(err => console.error('Error copiando al portapapeles:', err));
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