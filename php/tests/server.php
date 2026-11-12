<?php

$statePath = getenv('CC_TEST_STATE');
$state = json_decode(file_get_contents($statePath), true);
$state['requests'][] = ['method' => $_SERVER['REQUEST_METHOD'], 'uri' => $_SERVER['REQUEST_URI'], 'headers' => array_change_key_case(getallheaders()), 'body' => file_get_contents('php://input')];
$response = array_shift($state['responses']) ?? [200, []];
file_put_contents($statePath, json_encode($state));
http_response_code($response[0]);
header('Content-Type: application/json');
if (isset($response[2])) {
    header('Retry-After: ' . $response[2]);
}
echo json_encode($response[1]);
