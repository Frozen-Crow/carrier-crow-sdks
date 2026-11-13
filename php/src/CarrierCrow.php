<?php

declare(strict_types=1);

namespace CarrierCrow;

use GuzzleHttp\Client;
use GuzzleHttp\ClientInterface;
use GuzzleHttp\Exception\TransferException;

class CarrierCrow
{
    public const VERSION = '0.1.0';
    public readonly Emails $emails;
    public readonly Domains $domains;
    public readonly Webhooks $webhooks;
    private ClientInterface $http;
    private string $key;
    private array $options;

    public function __construct(?string $apiKey = null, array $options = [])
    {
        $this->key = $apiKey ?? (getenv('CARRIERCROW_API_KEY') ?: '');
        if ($this->key === '') {
            throw new CarrierCrowException('API key is required', codeName: 'missing_api_key');
        }
        $cap = $options['maxRetryAfter'] ?? 60;
        if ((!is_int($cap) && !is_float($cap)) || !is_finite((float) $cap) || $cap < 0) {
            throw new CarrierCrowException('maxRetryAfter must be finite and nonnegative', codeName: 'invalid_request');
        }
        $this->options = $options;
        $this->http = $options['httpClient'] ?? new Client();
        $this->emails = new Emails($this);
        $this->domains = new Domains($this);
        $this->webhooks = new Webhooks();
    }

    public function request(string $method, string $path, ?array $body = null, array $query = [], array $options = [], bool $send = false): array
    {
        $retries = $options['retries'] ?? $this->options['retries'] ?? 2;
        if (!is_int($retries) || $retries < 0) {
            throw new CarrierCrowException('Retries must be a nonnegative integer', codeName: 'invalid_request');
        }
        $key = $options['idempotencyKey'] ?? null;
        if ($key !== null && (!is_string($key) || strlen($key) < 1 || strlen($key) > 255)) {
            throw new CarrierCrowException('Idempotency key must contain 1–255 characters', codeName: 'invalid_request');
        }
        if ($send && !$key && ($options['retries'] ?? 0) > 0) {
            $key = bin2hex(random_bytes(16));
        }
        if ($send && !$key) {
            $retries = 0;
        }
        $headers = ['Authorization' => 'Bearer ' . $this->key, 'User-Agent' => 'carriercrow-php/' . self::VERSION, 'Accept' => 'application/json'];
        if ($key) {
            $headers['Idempotency-Key'] = $key;
        }
        $pairs = [];
        foreach ($query as $name => $values) {
            foreach (is_array($values) ? $values : [$values] as $value) {
                $pairs[] = rawurlencode($name) . '=' . rawurlencode((string)$value);
            }
        }
        $url = rtrim($this->options['baseUrl'] ?? 'https://api.mta.carriercrow.com', '/') . $path . ($pairs ? '?' . implode('&', $pairs) : '');
        $request = ['headers' => $headers, 'http_errors' => false, 'allow_redirects' => false, 'timeout' => $this->options['timeout'] ?? 30];
        if ($body !== null) {
            $request['body'] = json_encode($body, JSON_THROW_ON_ERROR);
            $request['headers']['Content-Type'] = 'application/json';
        }
        for ($attempt = 0; ; $attempt++) {
            $retryAfter = null;
            try {
                $response = $this->http->request($method, $url, $request);
                $status = $response->getStatusCode();
                $raw = (string) $response->getBody();
                $data = json_decode($raw, true);
                if ($status >= 200 && $status < 300) {
                    if (!is_array($data) || json_last_error() !== JSON_ERROR_NONE) {
                        throw new CarrierCrowException('Invalid JSON response', $status, 'invalid_response');
                    }
                    return $data;
                }
                if (!is_array($data)) {
                    $data = [];
                }
                $header = $response->getHeaderLine('Retry-After');
                $retryAfter = is_numeric($header) && is_finite((float) $header) && (float) $header >= 0 ? (float) $header : null;
                $error = new CarrierCrowException($data['error'] ?? "HTTP $status", $status, $data['code'] ?? null, $data['field'] ?? null, $retryAfter);
                if (!in_array($status, [429, 503], true) || $attempt >= $retries || ($retryAfter !== null && $retryAfter > ($this->options['maxRetryAfter'] ?? 60))) {
                    throw $error;
                }
            } catch (TransferException $cause) {
                if ($attempt >= $retries) {
                    throw new CarrierCrowException($cause->getMessage(), codeName: 'network_error');
                }
            }
            $delay = max($retryAfter ?? 0, 0.25 * (2 ** $attempt) * (0.5 + random_int(0, 1000) / 1000));
            if (isset($this->options['sleep'])) {
                ($this->options['sleep'])($delay);
            } else {
                usleep((int)($delay * 1000000));
            }
        }
    }
}
