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
            self::assertSame('carriercrow-php/1.0.0', $r['headers']['user-agent']);
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
