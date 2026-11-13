<?php

declare(strict_types=1);

namespace CarrierCrow;

class Domains
{
    public function __construct(private CarrierCrow $client)
    {
    }
    public function list(): array
    {
        return $this->client->request('GET', '/v1/domains');
    }
    public function get(string $id): array
    {
        return $this->client->request('GET', '/v1/domains/' . rawurlencode($id));
    }
    public function check(string $id): array
    {
        return $this->client->request('POST', '/v1/domains/' . rawurlencode($id) . '/check');
    }
}
