<?php
function vault_api($method, $endpoint, $data = null) {
    $url = "http://127.0.0.1:28085/api/v1" . $endpoint;
    $ch = curl_init($url);
    curl_setopt($ch, CURLOPT_RETURNTRANSFER, true);
    curl_setopt($ch, CURLOPT_CUSTOMREQUEST, strtoupper($method));
    curl_setopt($ch, CURLOPT_TIMEOUT, 30);
    if ($data !== null) {
        curl_setopt($ch, CURLOPT_POSTFIELDS, json_encode($data));
        curl_setopt($ch, CURLOPT_HTTPHEADER, ['Content-Type: application/json']);
    }
    $response = curl_exec($ch);
    $httpCode = curl_getinfo($ch, CURLINFO_HTTP_CODE);
    $error = curl_error($ch);
    curl_close($ch);

    if ($error) {
        return ['error' => $error];
    }
    return json_decode($response, true);
}

function vault_get($endpoint) { return vault_api('GET', $endpoint); }
function vault_post($endpoint, $data = null) { return vault_api('POST', $endpoint, $data); }
function vault_put($endpoint, $data) { return vault_api('PUT', $endpoint, $data); }
function vault_delete($endpoint) { return vault_api('DELETE', $endpoint); }
?>
