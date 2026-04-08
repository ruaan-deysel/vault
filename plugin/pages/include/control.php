<?php
// control.php — AJAX endpoint for Vault service start/stop/status.
// Called by Vault.page JavaScript to manage the daemon without full page reload.

header('Content-Type: application/json');

$RC = '/etc/rc.d/rc.vault';
$PIDFILE = '/var/run/vault.pid';
$CONFIG = '/boot/config/plugins/vault/vault.cfg';

function is_running() {
    global $PIDFILE;
    if (!file_exists($PIDFILE)) return false;
    $pid = trim(file_get_contents($PIDFILE));
    if (empty($pid)) return false;
    return posix_kill((int)$pid, 0);
}

$action = $_POST['action'] ?? $_GET['action'] ?? 'status';

switch ($action) {
    case 'start':
        exec("$RC start 2>&1", $out, $rc);
        // Brief wait for daemon to start.
        usleep(500000);
        echo json_encode(['running' => is_running(), 'output' => implode("\n", $out)]);
        break;

    case 'stop':
        exec("$RC stop 2>&1", $out, $rc);
        usleep(300000);
        echo json_encode(['running' => is_running(), 'output' => implode("\n", $out)]);
        break;

    case 'restart':
        exec("$RC restart 2>&1", $out, $rc);
        usleep(500000);
        echo json_encode(['running' => is_running(), 'output' => implode("\n", $out)]);
        break;

    case 'reset-config':
        // Reset vault.cfg to defaults, preserving SERVICE and SNAPSHOT_PATH.
        $service = 'yes';
        $snapshot = '';
        if (file_exists($CONFIG)) {
            $ini = @parse_ini_file($CONFIG, false, INI_SCANNER_RAW);
            if (is_array($ini)) {
                $service = $ini['SERVICE'] ?? 'yes';
                $snapshot = $ini['SNAPSHOT_PATH'] ?? '';
            }
        }
        // Sanitize values to prevent INI injection (remove newlines, quotes).
        $service = preg_replace('/["\'\r\n]/', '', $service);
        $snapshot = preg_replace('/["\'\r\n]/', '', $snapshot);
        $content = "SERVICE=\"{$service}\"\nPORT=\"24085\"\nBIND_ADDRESS=\"127.0.0.1\"\nSNAPSHOT_PATH=\"{$snapshot}\"\n";
        $written = file_put_contents($CONFIG, $content);
        if ($written === false) {
            http_response_code(500);
            echo json_encode(['error' => 'Failed to write config file']);
            break;
        }
        // Restart daemon if running so it picks up the defaults.
        $was_running = is_running();
        if ($was_running) {
            exec("$RC restart 2>&1", $out, $rc);
            usleep(500000);
        }
        echo json_encode(['running' => is_running(), 'reset' => true]);
        break;

    case 'status':
        echo json_encode(['running' => is_running()]);
        break;

    default:
        http_response_code(400);
        echo json_encode(['error' => 'Invalid action']);
}
