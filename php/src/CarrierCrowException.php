<?php

declare(strict_types=1);

namespace CarrierCrow;

class CarrierCrowException extends \RuntimeException
{
    public readonly ?string $errorCode;

    public function __construct(string $message, public readonly ?int $status = null, public readonly ?string $codeName = null, public readonly ?string $field = null, public readonly ?float $retryAfter = null)
    {
        parent::__construct($message);
        $this->errorCode = $codeName;
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
