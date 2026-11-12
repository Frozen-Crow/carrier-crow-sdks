require 'minitest/autorun'
require 'socket'
require_relative '../lib/carriercrow'

class ClientTest < Minitest::Test
  def setup
    @server = TCPServer.new('127.0.0.1', 0)
    @requests, @responses = [], []
    @thread = Thread.new do
      loop do
        socket = @server.accept
        first = socket.gets
        headers = {}
        while (line = socket.gets) && line != "\r\n"
          k, v = line.split(':', 2)
          headers[k.downcase] = v.strip
        end
        body = socket.read(headers.fetch('content-length', '0').to_i)
        @requests << [first.split[0..1], headers, body]
        status, payload, extra = @responses.shift || [200, {}]
        if status == :disconnect
          socket.close
          next
        end
        encoded = JSON.generate(payload)
        response = "HTTP/1.1 #{status} Test\r\nContent-Type: application/json\r\n"
        response += "Content-Length: #{encoded.bytesize}\r\nConnection: close\r\n#{extra}\r\n#{encoded}"
        socket.write(response)
        socket.close
      end
    rescue IOError, Errno::EBADF
    end
    @delays = []
    @client = CarrierCrow::Client.new(
      'ccm_test', base_url: "http://127.0.0.1:#{@server.addr[1]}", sleeper: ->(seconds) { @delays << seconds }
    )
  end

  def teardown
    @server.close
    @thread.join
  end

  def test_every_method
    @client.emails.send({ from: 'a', text: 'hello' })
    @client.emails.batch([{ to: 'b' }])
    @client.emails.get('a/b')
    @client.emails.list(status: %w[delivered bounced], q: 'a b', tenant: 't', app: 'a', days: 2, before: 'c', limit: 10)
    @client.domains.list
    @client.domains.get('a/b')
    @client.domains.check('a/b')
    assert_equal %w[POST POST GET GET GET GET POST], @requests.map { |r| r[0][0] }
    expected = ['/v1/emails', '/v1/emails/batch', '/v1/emails/a%2Fb', '/v1/emails',
                '/v1/domains', '/v1/domains/a%2Fb', '/v1/domains/a%2Fb/check']
    assert_equal expected, @requests.map { |r| r[0][1].split('?').first }
    assert_equal({ 'from' => 'a', 'text' => 'hello' }, JSON.parse(@requests[0][2]))
    assert_equal 'application/json', @requests[0][1]['content-type']
    assert_equal '/v1/emails/a%2Fb', @requests[2][0][1]
    assert_includes @requests[3][0][1], 'status=delivered&status=bounced'
    assert_equal({ 'emails' => [{ 'to' => 'b' }] }, JSON.parse(@requests[1][2]))
    @requests.each do |_, headers, _|
      assert_equal 'Bearer ccm_test', headers['authorization']
      assert_equal 'carriercrow-ruby/1.0.0', headers['user-agent']
    end
  end

  def test_error
    @responses << [400, { error: 'Fix to', code: 'invalid_request', field: 'to' }, "Retry-After: 3\r\n"]
    error = assert_raises(CarrierCrow::Error) { @client.domains.list }
    assert_equal [400, 'invalid_request', 'to', 3.0, 'Fix to'],
                 [error.status, error.code, error.field, error.retry_after, error.message]
    assert_equal 1, @requests.length
  end

  def test_retries
    [429, 503, :disconnect].each do |status|
      @responses.concat([[status, { error: 'later' }, "Retry-After: 0\r\n"], [200, { ok: true }]])
      assert @client.domains.list['ok']
    end
    assert_equal 6, @requests.length
    @responses.concat([[503, {}], [200, {}]])
    @client.emails.send({ text: 'x' }, retries: 1)
    assert_equal @requests[-1][1]['idempotency-key'], @requests[-2][1]['idempotency-key']
    refute_nil @requests[-1][1]['idempotency-key']
    assert_equal @requests[-1][2], @requests[-2][2]
  end

  def test_no_key_send_and_network
    [503, :disconnect].each do |status|
      @responses << [status, {}]
      assert_raises(CarrierCrow::Error) { @client.emails.send({ text: 'x' }) }
    end
    assert_equal 2, @requests.length
    @responses.concat([[503, {}], [200, {}]])
    @client.emails.batch([], idempotency_key: 'fixed')
    assert_equal %w[fixed fixed], @requests.last(2).map { |r| r[1]['idempotency-key'] }
  end

  def test_paging
    @responses.concat([[200, { emails: [{ id: 'one' }], next: 'cursor' }],
                       [200, { emails: [{ id: 'two' }], next: nil }]])
    assert_equal %w[one two], @client.emails.each(status: ['delivered']).map { |e| e['id'] }
    assert_includes @requests.last[0][1], 'before=cursor'
    assert_includes @requests.last[0][1], 'status=delivered'
  end

  def test_all_error_codes_and_no_retry_for_other_statuses
    { 400 => 'invalid_request', 401 => 'unauthorized', 403 => 'forbidden',
      404 => 'not_found', 409 => 'idempotency_conflict', 413 => 'too_large',
      422 => 'rejected', 500 => 'unexpected' }.each do |status, code|
      @responses << [status, { error: 'Fix request', code: code, field: 'to[2]' }]
      error = assert_raises(CarrierCrow::Error) { @client.domains.list }
      assert_equal [status, code, 'to[2]', 'Fix request'], [error.status, error.code, error.field, error.message]
    end
    @responses << [403, { error: 'Wrong app', code: 'app_mismatch' }]
    assert_equal 'app_mismatch', assert_raises(CarrierCrow::Error) { @client.domains.list }.code
    assert_equal 9, @requests.length
    assert_empty @delays
  end

  def test_retry_exhaustion_and_retry_after
    3.times { @responses << [429, { error: 'Slow down', code: 'rate_limited' }, "Retry-After: 3\r\n"] }
    error = assert_raises(CarrierCrow::Error) { @client.domains.list }
    assert_equal [429, 'rate_limited', 3.0], [error.status, error.code, error.retry_after]
    assert_equal [3.0, 3.0], @delays
    assert_equal 3, @requests.length
  end

  def test_network_exhaustion_is_typed
    3.times { @responses << [:disconnect, {}] }
    error = assert_raises(CarrierCrow::Error) { @client.domains.list }
    assert_equal 'network_error', error.code
    assert_nil error.status
    assert_equal 3, @requests.length
    assert_equal 2, @delays.length
  end

  def test_both_sends_are_safe_and_opt_in_retries_keep_key_and_bytes
    [:send, :batch].each do |method|
      body = method == :send ? { text: 'café' } : [{ text: 'café' }]
      [429, 503, :disconnect].each do |status|
        start = @requests.length
        @responses << [status, {}]
        assert_raises(CarrierCrow::Error) { @client.emails.public_send(method, body) }
        assert_equal start + 1, @requests.length
        assert_nil @requests.last[1]['idempotency-key']
      end
      @responses.concat([[:disconnect, {}], [200, { ok: true }]])
      assert @client.emails.public_send(method, body, retries: 1)['ok']
      first, second = @requests.last(2)
      assert_match(/\A[0-9a-f-]{36}\z/, first[1]['idempotency-key'])
      assert_equal first[1]['idempotency-key'], second[1]['idempotency-key']
      assert_equal first[2], second[2]
    end
  end

  def test_explicit_key_can_disable_retries_and_invalid_options_fail_before_http
    @responses << [503, {}]
    assert_raises(CarrierCrow::Error) { @client.emails.send({}, idempotency_key: 'fixed', retries: 0) }
    ['', 'x' * 256, 1].each do |key|
      assert_raises(ArgumentError) { @client.emails.send({}, idempotency_key: key) }
    end
    assert_raises(ArgumentError) { @client.emails.send({}, retries: -1) }
    assert_equal 1, @requests.length
  end

  def test_string_cursor_is_replaced_without_mutating_query
    query = { 'before' => 'old', 'q' => 'hello' }
    @responses.concat([[200, { emails: [], next: 'new' }], [200, { emails: [], next: nil }]])
    assert_empty @client.emails.each(query).to_a
    assert_equal [['before', 'new'], ['q', 'hello']], URI.decode_www_form(URI(@requests.last[0][1]).query)
    assert_equal 'old', query['before']
  end

  def test_vectors
    vectors = JSON.parse(File.read(File.expand_path('../../test-vectors/webhooks.json', __dir__)))
    vectors['cases'].each do |c|
      options = { now: c.fetch('now', vectors['now']), tolerance: c.fetch('tolerance', 300) }
      if c['valid']
        assert_equal JSON.parse(c['body']),
                     CarrierCrow::Webhooks.verify(c['body'], c['signature'], vectors['secret'], **options), c['name']
      else
        assert_raises(CarrierCrow::Error, c['name']) { CarrierCrow::Webhooks.verify(c['body'], c['signature'], vectors['secret'], **options) }
      end
    end
  end
end
