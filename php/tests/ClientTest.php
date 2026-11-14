<?php

require_once __DIR__ . '/../vendor/autoload.php';

use CarrierCrow\CarrierCrow;
use CarrierCrow\Error;
use CarrierCrow\Webhooks;
use PHPUnit\Framework\TestCase;

final class ClientTest extends TestCase
{
    private $process;
    private string $state;
    private CarrierCrow $client;
    protected function setUp(): void
    {
        $this->state = tempnam(sys_get_temp_dir(), 'cc-test');
        file_put_contents($this->state, json_encode(['requests' => [], 'responses' => []]));
        $socket = stream_socket_server('tcp://127.0.0.1:0', $errno, $error);
        $address = stream_socket_get_name($socket, false);
        fclose($socket);
        $this->process = proc_open([PHP_BINARY, '-S', $address, __DIR__ . '/server.php'], [['pipe','r'], ['file','/dev/null','a'], ['file','/dev/null','a']], $pipes, null, array_merge(getenv(), ['CC_TEST_STATE' => $this->state]));
        for ($i = 0; $i < 100; $i++) {
            $connection = @stream_socket_client('tcp://' . $address);
            if ($connection) {
                fclose($connection);
                break;
            } usleep(10000);
        }
        $this->client = new CarrierCrow('ccm_test', ['baseUrl' => 'http://' . $address, 'sleep' => static function ($delay) {
        }]);
    }
    protected function tearDown(): void
    {
        proc_terminate($this->process);
        proc_close($this->process);
        unlink($this->state);
    }
    private function state(): array
    {
        return json_decode(file_get_contents($this->state), true);
    }
    private function responses(array $responses): void
    {
        $state = $this->state();
        $state['responses'] = $responses;
        file_put_contents($this->state, json_encode($state));
    }
    public function testOptionalStream(): void
    {
        $email = ['from' => 'a@example.com', 'to' => 'b@example.com', 'subject' => 'Hi', 'text' => 'Hello'];
        $this->client->emails->send($email + ['stream' => 'receipts']);
        $this->client->emails->send($email);
        $this->client->emails->batch([$email + ['stream' => 'receipts'], $email + ['stream' => 'news'], $email]);
        $requests = $this->state()['requests'];
        self::assertSame('receipts', json_decode($requests[0]['body'], true)['stream']);
        self::assertArrayNotHasKey('stream', json_decode($requests[1]['body'], true));
        $emails = json_decode($requests[2]['body'], true)['emails'];
        self::assertSame('receipts', $emails[0]['stream']);
        self::assertSame('news', $emails[1]['stream']);
        self::assertArrayNotHasKey('stream', $emails[2]);
    }
    public function testNullRecipientId(): void
    {
        $accepted = ['status' => 'accepted', 'message_id' => 'order-1', 'recipients' => [['address' => 'b@example.com', 'id' => null]]];
        $this->responses([[202, ['object' => 'email'] + $accepted], [200, ['results' => [['index' => 0] + $accepted]]]]);
        $sent = $this->client->emails->send([]);
        self::assertArrayHasKey('id', $sent['recipients'][0]);
        self::assertNull($sent['recipients'][0]['id']);
        self::assertSame('order-1', $sent['message_id']);
        $batch = $this->client->emails->batch([[]]);
        self::assertArrayHasKey('id', $batch['results'][0]['recipients'][0]);
        self::assertNull($batch['results'][0]['recipients'][0]['id']);
        self::assertCount(2, $this->state()['requests']);
    }
    public function testRecipientRefusalField(): void
    {
        $this->responses([[422, ['error' => 'Recipient refused', 'code' => 'rejected', 'field' => 'to[1]']]]);
        try {
            $this->client->emails->send([]);
            self::fail();
        } catch (Error $e) {
            self::assertSame(422, $e->status);
            self::assertSame('rejected', $e->codeName);
            self::assertSame('to[1]', $e->field);
        }
        self::assertCount(1, $this->state()['requests']);
    }
    public function testEveryMethod(): void
    {
        $this->client->emails->send(['from' => 'a', 'text' => 'hello']);
        $this->client->emails->batch([['to' => 'b']]);
        $this->client->emails->get('a/b');
        $this->client->emails->list(['status' => ['delivered', 'bounced'], 'q' => 'a b', 'tenant' => 't', 'app' => 'a', 'days' => 2, 'before' => 'c', 'limit' => 10]);
        $this->client->domains->list();
        $this->client->domains->get('a/b');
        $this->client->domains->check('a/b');
        $requests = $this->state()['requests'];
        self::assertSame(['POST','POST','GET','GET','GET','GET','POST'], array_column($requests, 'method'));
        self::assertSame(['/v1/emails', '/v1/emails/batch', '/v1/emails/a%2Fb', '/v1/emails', '/v1/domains', '/v1/domains/a%2Fb', '/v1/domains/a%2Fb/check'], array_map(static fn ($r) => explode('?', $r['uri'])[0], $requests));
        self::assertSame(['from' => 'a', 'text' => 'hello'], json_decode($requests[0]['body'], true));
        self::assertSame('application/json', $requests[0]['headers']['content-type']);
        self::assertStringContainsString('status=delivered&status=bounced', $requests[3]['uri']);
        self::assertSame(['emails' => [['to' => 'b']]], json_decode($requests[1]['body'], true));
        foreach ($requests as $r) {
            self::assertSame('Bearer ccm_test', $r['headers']['authorization']);
            self::assertSame('carriercrow-php/0.2.0', $r['headers']['user-agent']);
        }
    }
    public function testError(): void
    {
        $this->responses([[400, ['error' => 'Fix to', 'code' => 'invalid_request', 'field' => 'to'], 3]]);
        try {
            $this->client->domains->list();
            self::fail();
        } catch (Error $e) {
            self::assertSame([400, 'invalid_request', 'to', 3.0, 'Fix to'], [$e->status, $e->code, $e->field, $e->retryAfter, $e->getMessage()]);
        }
        self::assertCount(1, $this->state()['requests']);
    }
    public function testRetries(): void
    {
        foreach ([429,503] as $status) {
            $this->responses([[$status, [], 0], [200, ['ok' => true]]]);
            self::assertTrue($this->client->domains->list()['ok']);
        }
        $this->responses([[503, []], [200, []]]);
        $this->client->emails->send(['text' => 'x'], ['retries' => 1]);
        $requests = array_slice($this->state()['requests'], -2);
        self::assertNotEmpty($requests[0]['headers']['idempotency-key']);
        self::assertSame($requests[0]['headers']['idempotency-key'], $requests[1]['headers']['idempotency-key']);
        self::assertSame($requests[0]['body'], $requests[1]['body']);
        $this->responses([[503, []]]);
        try {
            $this->client->emails->send([]);
            self::fail();
        } catch (Error $e) {
            self::assertSame(503, $e->status);
        }
        self::assertCount(7, $this->state()['requests']);
        $this->responses([[503, []], [200, []]]);
        $this->client->emails->batch([], ['idempotencyKey' => 'fixed']);
        foreach (array_slice($this->state()['requests'], -2) as $r) {
            self::assertSame('fixed', $r['headers']['idempotency-key']);
        }
    }
    public function testNetwork(): void
    {
        $calls = 0;
        $handler = new \GuzzleHttp\Handler\MockHandler([new \GuzzleHttp\Exception\ConnectException('offline', new \GuzzleHttp\Psr7\Request('GET', '/')), new \GuzzleHttp\Psr7\Response(200, [], '{"ok":true}')]);
        $client = new CarrierCrow('ccm_test_key', ['httpClient' => new \GuzzleHttp\Client(['handler' => $handler]), 'sleep' => static function ($delay) use (&$calls) {
            $calls++;
        }]);
        self::assertTrue($client->domains->list()['ok']);
        self::assertSame(1, $calls);
        $handler->append(new \GuzzleHttp\Exception\ConnectException('offline', new \GuzzleHttp\Psr7\Request('POST', '/')));
        try {
            $client->emails->send([]);
            self::fail();
        } catch (Error $e) {
            self::assertSame('network_error', $e->code);
        }
    }
    public function testLocalNetworkRetryExhaustion(): void
    {
        $socket = stream_socket_server('tcp://127.0.0.1:0', $errno, $error);
        $address = stream_socket_get_name($socket, false);
        fclose($socket);
        $sleeps = 0;
        $client = new CarrierCrow('ccm_test_key', ['baseUrl' => 'http://' . $address, 'timeout' => 0.1, 'sleep' => static function ($delay) use (&$sleeps) {
            $sleeps++;
        }]);
        try {
            $client->domains->list();
            self::fail();
        } catch (Error $e) {
            self::assertSame('network_error', $e->code);
        }
        self::assertSame(2, $sleeps);
        try {
            $client->emails->send([]);
            self::fail();
        } catch (Error $e) {
            self::assertSame('network_error', $e->code);
        }
        self::assertSame(2, $sleeps);
    }
    public function testAllHttpErrorsAndExhaustion(): void
    {
        foreach ([400, 401, 403, 404, 409, 413, 422] as $status) {
            $this->responses([[$status, ['error' => 'Request refused', 'code' => 'test_code']]]);
            try {
                $this->client->domains->list();
                self::fail();
            } catch (Error $e) {
                self::assertSame($status, $e->status);
            }
        }
        $this->responses([[503, []], [503, []], [503, []]]);
        try {
            $this->client->domains->list();
            self::fail();
        } catch (Error $e) {
            self::assertSame(503, $e->status);
        }
        self::assertCount(10, $this->state()['requests']);
    }
    public function testPaging(): void
    {
        $this->responses([[200, ['emails' => [['id' => 'one']], 'next' => 'cursor']], [200, ['emails' => [['id' => 'two']], 'next' => null]]]);
        self::assertSame(['one', 'two'], array_column(iterator_to_array($this->client->emails->all(['status' => ['delivered']]), false), 'id'));
        $last = $this->state()['requests'][1];
        self::assertStringContainsString('before=cursor', $last['uri']);
        self::assertStringContainsString('status=delivered', $last['uri']);
    }
    public function testRetryAfterDelayAndBatchSafety(): void
    {
        $delays = [];
        $property = new \ReflectionProperty($this->client, 'options');
        $options = $property->getValue($this->client);
        $options['sleep'] = static function ($delay) use (&$delays) {
            $delays[] = $delay;
        };
        $property->setValue($this->client, $options);
        $this->responses([[429, [], 4], [200, []]]);
        $this->client->domains->list();
        self::assertSame([4.0], $delays);
        $this->responses([[503, []]]);
        try {
            $this->client->emails->batch([]);
            self::fail();
        } catch (Error $e) {
            self::assertSame(503, $e->status);
        }
        self::assertCount(3, $this->state()['requests']);
    }
    public function testMissingKeyAndLegacyAlias(): void
    {
        $saved = getenv('CARRIERCROW_API_KEY');
        putenv('CARRIERCROW_API_KEY');
        try {
            try {
                new CarrierCrow();
                self::fail('Missing key must fail at construction');
            } catch (\CarrierCrow\CarrierCrowException $e) {
                self::assertSame('missing_api_key', $e->errorCode);
                self::assertSame($e->errorCode, $e->code);
                self::assertInstanceOf(Error::class, $e);
            }
            putenv('CARRIERCROW_API_KEY=ccm_test_env');
            self::assertInstanceOf(CarrierCrow::class, new CarrierCrow());
        } finally {
            putenv($saved === false ? 'CARRIERCROW_API_KEY' : 'CARRIERCROW_API_KEY=' . $saved);
        }
    }
    public function testRetryAfterCap(): void
    {
        foreach ([429, 503] as $status) {
            foreach ([[], ['maxRetryAfter' => 2]] as $options) {
                $cap = $options['maxRetryAfter'] ?? 60;
                $delays = [];
                $handler = new \GuzzleHttp\Handler\MockHandler([
                    new \GuzzleHttp\Psr7\Response($status, ['Retry-After' => (string) $cap], '{}'),
                    new \GuzzleHttp\Psr7\Response(200, [], '{}'),
                    new \GuzzleHttp\Psr7\Response($status, ['Retry-After' => (string) ($cap + 1)], '{"error":"later","code":"unavailable","field":"to"}'),
                ]);
                $client = new CarrierCrow('ccm_test', $options + [
                    'httpClient' => new \GuzzleHttp\Client(['handler' => $handler]),
                    'sleep' => static function ($delay) use (&$delays) {
                        $delays[] = $delay;
                    },
                ]);
                $client->domains->list();
                self::assertSame([(float) $cap], $delays);
                try {
                    $client->domains->list();
                    self::fail('Over-cap response must be raised');
                } catch (\CarrierCrow\CarrierCrowException $e) {
                    self::assertSame([$status, 'unavailable', 'to', (float) ($cap + 1)], [$e->status, $e->errorCode, $e->field, $e->retryAfter]);
                }
                self::assertCount(1, $delays);
                self::assertCount(0, $handler);
            }
        }
    }
    public function testPagingRejectsCursorCycles(): void
    {
        foreach ([['loop', 'loop'], ['one', 'two', 'one']] as $cursors) {
            $this->responses(array_map(static fn ($next) => [200, ['emails' => [], 'next' => $next]], $cursors));
            try {
                iterator_to_array($this->client->emails->all());
                self::fail('Cursor cycle must fail');
            } catch (\CarrierCrow\CarrierCrowException $e) {
                self::assertSame('invalid_response', $e->errorCode);
            }
        }
        self::assertCount(5, $this->state()['requests']);
    }
    public function testFirstTimestampWins(): void
    {
        $body = '{"webhook":"wh_test","delivery":"del_test","events":[]}';
        $digest = hash_hmac('sha256', '100.' . $body, 'whsec_test');
        self::assertSame(json_decode($body, true), Webhooks::verify($body, "t=100,t=101,v1=$digest", 'whsec_test', ['now' => 100]));
        foreach (["t=101,t=100,v1=$digest", "t=bad,t=100,v1=$digest", 't=100,v1=' . strtoupper($digest)] as $header) {
            try {
                Webhooks::verify($body, $header, 'whsec_test', ['now' => 100]);
                self::fail('Invalid signature must fail');
            } catch (\CarrierCrow\CarrierCrowException $e) {
                self::assertSame('invalid_signature', $e->errorCode);
            }
        }
    }
    public function testVectors(): void
    {
        $vectors = json_decode(file_get_contents(__DIR__ . '/../../test-vectors/webhooks.json'), true, 512, JSON_THROW_ON_ERROR);
        foreach ($vectors['cases'] as $case) {
            $options = ['now' => $case['now'] ?? $vectors['now'], 'tolerance' => $case['tolerance'] ?? 300];
            try {
                $actual = Webhooks::verify($case['body'], $case['signature'], $vectors['secret'], $options);
                self::assertTrue($case['valid'], $case['name']);
                self::assertSame(json_decode($case['body'], true), $actual);
            } catch (Error $e) {
                self::assertFalse($case['valid'], $case['name']);
            }
        }
    }
}
