<?php

namespace CarrierCrow;

class Emails
{
    public function __construct(private CarrierCrow $client)
    {
    }
    public function send(array $params, array $options = []): array
    {
        return $this->client->request('POST', '/v1/emails', $params, options: $options, send: true);
    }
    public function batch(array $emails, array $options = []): array
    {
        return $this->client->request('POST', '/v1/emails/batch', ['emails' => $emails], options: $options, send: true);
    }
    public function get(string $id): array
    {
        return $this->client->request('GET', '/v1/emails/' . rawurlencode($id));
    }
    public function list(array $query = []): array
    {
        return $this->client->request('GET', '/v1/emails', query: $query);
    }
    public function all(array $query = []): \Generator
    {
        do {
            $page = $this->list($query);
            foreach ($page['emails'] as $email) {
                yield $email;
            }
            $query['before'] = $page['next'] ?? null;
        } while ($query['before'] !== null);
    }
}
