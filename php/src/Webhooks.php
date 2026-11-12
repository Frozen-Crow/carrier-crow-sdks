<?php

namespace CarrierCrow;

class Webhooks
{
    public static function verify(string $body, string $signature, string $secret, array $options = []): array
    {
        $timestamp = null;
        $signatures = [];
        foreach (explode(',', $signature) as $part) {
            $pair = explode('=', trim($part), 2);
            if (count($pair) !== 2) {
                continue;
            }
            if ($pair[0] === 't') {
                if ($timestamp !== null) {
                    throw new Error('Multiple webhook timestamps', codeName: 'invalid_signature');
                }
                $timestamp = $pair[1];
            }
            if ($pair[0] === 'v1') {
                $signatures[] = $pair[1];
            }
        }
        $now = $options['now'] ?? time();
        $tolerance = $options['tolerance'] ?? 300;
        $valid = $timestamp !== null && ctype_digit($timestamp)
            && is_finite((float) $now) && is_finite((float) $tolerance) && $tolerance >= 0
            && abs($now - (float) $timestamp) <= $tolerance;
        $digest = hash_hmac('sha256', $timestamp . '.' . $body, $secret);
        $matched = false;
        foreach ($signatures as $candidate) {
            if (preg_match('/^[0-9a-fA-F]{64}$/D', $candidate) && hash_equals($digest, strtolower($candidate))) {
                $matched = true;
            }
        }
        if (!$valid || !$matched) {
            throw new Error('Invalid webhook signature', codeName: 'invalid_signature');
        }
        try {
            $data = json_decode($body, true, 512, JSON_THROW_ON_ERROR);
        } catch (\JsonException $e) {
            throw new Error('Invalid webhook JSON', codeName: 'invalid_payload');
        }
        if (!is_array($data) || !is_string($data['webhook'] ?? null)
            || !is_string($data['delivery'] ?? null) || !is_array($data['events'] ?? null)) {
            throw new Error('Invalid webhook envelope', codeName: 'invalid_payload');
        }
        return $data;
    }
}
