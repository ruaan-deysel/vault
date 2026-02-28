function showCreateJobModal() {
    showModal('createJobModal');
}

document.getElementById('createJobForm')?.addEventListener('submit', async function(e) {
    e.preventDefault();
    const form = new FormData(this);
    const job = {
        name: form.get('name'),
        description: form.get('description'),
        schedule: form.get('schedule'),
        backup_type_chain: form.get('backup_type_chain'),
        compression: form.get('compression'),
        storage_dest_id: parseInt(form.get('storage_dest_id')),
        retention_count: parseInt(form.get('retention_count')),
        retention_days: parseInt(form.get('retention_days')),
        enabled: true,
    };

    const result = await vaultAPI('POST', '/jobs', job);
    if (result.error) {
        alert('Error: ' + result.error);
    } else {
        hideModal('createJobModal');
        location.reload();
    }
});

async function runJob(id) {
    if (!confirm('Run this backup job now?')) return;
    const result = await vaultAPI('POST', '/jobs/' + id + '/run');
    if (result.error) {
        alert('Error: ' + result.error);
    } else {
        alert('Job started');
    }
}

async function deleteJob(id) {
    if (!confirm('Delete this backup job? This cannot be undone.')) return;
    await vaultAPI('DELETE', '/jobs/' + id);
    location.reload();
}

function editJob(id) {
    // TODO: populate modal with existing job data
    alert('Edit functionality coming soon');
}
