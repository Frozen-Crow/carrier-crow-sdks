<?php

namespace CarrierCrow;

class Error extends \RuntimeException
{
    public function __construct(string $message, public readonly ?int $status = null, public readonly ?string $codeName = null, public readonly ?string $field = null, public readonly ?float $retryAfter = null)
    {
        parent::__construct($message);
    }

    public function __get(string $name): mixed
    {
        return match ($name) {
            'code' => $this->codeName,
            'message' => $this->getMessage(),
            default => null,
        };
    }
}
