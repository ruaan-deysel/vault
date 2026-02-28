const VAULT_API = '/plugins/vault/api.php';

async function vaultAPI(method, endpoint, data) {
    const opts = {
        method: method,
        headers: { 'Content-Type': 'application/json' },
    };
    if (data) opts.body = JSON.stringify(data);

    const resp = await fetch('http://127.0.0.1:28085/api/v1' + endpoint, opts);
    return resp.json();
}

function showModal(id) {
    document.getElementById(id).style.display = 'flex';
}

function hideModal(id) {
    document.getElementById(id).style.display = 'none';
}

function formatBytes(bytes) {
    if (bytes === 0) return '0 B';
    const k = 1024;
    const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
}

// WebSocket for real-time progress
let ws = null;

function connectWebSocket() {
    ws = new WebSocket('ws://127.0.0.1:28085/api/v1/ws');
    ws.onmessage = function(event) {
        const data = JSON.parse(event.data);
        if (data.type === 'progress') {
            updateProgress(data);
        }
    };
    ws.onclose = function() {
        setTimeout(connectWebSocket, 5000);
    };
}

function updateProgress(data) {
    const el = document.getElementById('progress-' + data.item);
    if (el) {
        el.style.width = data.percent + '%';
        el.textContent = data.message || (data.percent + '%');
    }
}

document.addEventListener('DOMContentLoaded', connectWebSocket);
