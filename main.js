const { app, BrowserWindow, ipcMain, dialog } = require('electron')
const path = require('path')
const { exec } = require('child_process')
const fs = require('fs')
const os = require('os')

function createWindow() {
    const win = new BrowserWindow({
        width: 800,
        height: 600,
        webPreferences: {
            nodeIntegration: false,
            contextIsolation: true,
            preload: path.join(__dirname, 'preload.js'),
            webSecurity: true,
            allowRunningInsecureContent: false
        }
    })

    // Configurar CSP de manera más segura
    win.webContents.session.webRequest.onHeadersReceived((details, callback) => {
        callback({
            responseHeaders: {
                ...details.responseHeaders,
                'Content-Security-Policy': [
                    "default-src 'self';" +
                    "script-src 'self' 'unsafe-inline' 'unsafe-eval';" + // Permitir scripts en línea
                    "style-src 'self' 'unsafe-inline';" +
                    "connect-src 'self' http://localhost:* ws://localhost:*;" +
                    "img-src 'self' data: https:;" +
                    "worker-src 'self' blob:;" +
                    "frame-src 'none';"
                ]
            }
        })
    })

    win.loadFile('index.html')

    if (process.env.NODE_ENV === 'development') {
        win.webContents.openDevTools()
    }
}

// Manejar la selección de archivos
ipcMain.handle('select-file', async () => {
    const result = await dialog.showOpenDialog({
        properties: ['openFile'],
        filters: [
            { name: 'Ejecutables', extensions: ['exe'] }
        ]
    })

    if (!result.canceled && result.filePaths.length > 0) {
        return result.filePaths[0]
    }
    return null
})

// Manejar el lanzamiento en modo aislado
ipcMain.handle('launch-in-sandbox', async (event, gamePath) => {
    const gameDir = path.dirname(gamePath)

    // Crear un script PowerShell para ejecutar el proceso aislado
    const psScript = `
$ProcessInfo = New-Object System.Diagnostics.ProcessStartInfo
$ProcessInfo.FileName = "${gamePath.replace(/\\/g, '\\\\')}"
$ProcessInfo.WorkingDirectory = "${gameDir.replace(/\\/g, '\\\\')}"
$ProcessInfo.UseShellExecute = $false
$ProcessInfo.RedirectStandardOutput = $true
$ProcessInfo.RedirectStandardError = $true
$ProcessInfo.CreateNoWindow = $false

# Configurar argumentos para Warcraft III
$ProcessInfo.Arguments = "-window -nativefullscr -creategame"

# Crear y configurar el proceso
$Process = New-Object System.Diagnostics.Process
$Process.StartInfo = $ProcessInfo

# Iniciar el proceso
$Process.Start()

# Obtener el ID del proceso principal
$mainProcessId = $Process.Id
Write-Host "Proceso principal ID: $mainProcessId"

# Crear un Job para aislar el proceso
$job = Start-Job -ScriptBlock {
    $Process = Get-Process -Id $using:mainProcessId
    $Process.PriorityClass = 'Normal'
    
    # Configurar afinidad del procesador (usar solo el primer núcleo)
    $Process.ProcessorAffinity = 1
    
    # Esperar a que termine
    $Process.WaitForExit()
    
    # Obtener procesos hijos
    $childProcesses = Get-WmiObject Win32_Process | Where-Object { $_.ParentProcessId -eq $using:mainProcessId }
    foreach ($childProcess in $childProcesses) {
        Write-Host "Terminando proceso hijo: $($childProcess.ProcessId)"
        Stop-Process -Id $childProcess.ProcessId -Force
    }
}

# Esperar a que termine el job
Wait-Job $job
Remove-Job $job

# Asegurarse de que el proceso principal termine
Stop-Process -Id $mainProcessId -Force -ErrorAction SilentlyContinue
    `

    const scriptPath = path.join(os.tmpdir(), 'launch_game.ps1')

    try {
        // Guardar el script
        await fs.promises.writeFile(scriptPath, psScript, 'utf8')

        // Ejecutar PowerShell con el script
        return new Promise((resolve) => {
            exec(`powershell -ExecutionPolicy Bypass -File "${scriptPath}"`, (error, stdout, stderr) => {
                if (error) {
                    console.error('Error ejecutando el juego:', error)
                    console.error('Stderr:', stderr)
                    resolve(false)
                    return
                }
                console.log('Juego iniciado:', stdout)
                resolve(true)
            })
        })
    } catch (err) {
        console.error('Error configurando el lanzamiento:', err)
        return false
    }
})

app.whenReady().then(() => {
    createWindow()
})

app.on('window-all-closed', () => {
    if (process.platform !== 'darwin') {
        app.quit()
    }
})

app.on('activate', () => {
    if (BrowserWindow.getAllWindows().length === 0) {
        createWindow()
    }
}) 