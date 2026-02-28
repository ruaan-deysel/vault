async function loadRestorePoints(jobId) {
    if (!jobId) {
        document.getElementById('restorePointsTable').style.display = 'none';
        return;
    }

    const rps = await vaultAPI('GET', '/jobs/' + jobId + '/restore-points');
    const tbody = document.getElementById('restorePointsBody');
    tbody.innerHTML = '';

    if (rps && rps.length > 0) {
        rps.forEach(function(rp) {
            const tr = document.createElement('tr');
            tr.innerHTML =
                '<td>' + rp.created_at + '</td>' +
                '<td>' + rp.backup_type + '</td>' +
                '<td>' + formatBytes(rp.size_bytes) + '</td>' +
                '<td><button class="vault-btn vault-btn-sm vault-btn-primary" onclick="restoreFromPoint(' + rp.id + ')">Restore</button></td>';
            tbody.appendChild(tr);
        });
        document.getElementById('restorePointsTable').style.display = 'table';
    } else {
        tbody.innerHTML = '<tr><td colspan="4">No restore points found</td></tr>';
        document.getElementById('restorePointsTable').style.display = 'table';
    }
}

function restoreFromPoint(rpId) {
    if (!confirm('Are you sure you want to restore from this point? This will overwrite current data.')) return;
    alert('Restore functionality will be implemented');
}
