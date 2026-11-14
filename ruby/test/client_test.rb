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

  def test_optional_stream
    email = { from: 'a@example.com', to: 'b@example.com', subject: 'Hi', text: 'Hello' }
    @client.emails.send(email.merge(stream: 'receipts'))
    @client.emails.send(email)
    @client.emails.batch([email.merge(stream: 'receipts'), email.merge(stream: 'news'), email])
    assert_equal 'receipts', JSON.parse(@requests[0][2])['stream']
    refute JSON.parse(@requests[1][2]).key?('stream')
    emails = JSON.parse(@requests[2][2])['emails']
    assert_equal 'receipts', emails[0]['stream']
    assert_equal 'news', emails[1]['stream']
    refute emails[2].key?('stream')
  end

  def test_null_recipient_id
    accepted = { status: 'accepted', message_id: 'order-1', recipients: [{ address: 'b@example.com', id: nil }] }
    @responses.concat([[202, { object: 'email' }.merge(accepted)],
                       [200, { results: [{ index: 0 }.merge(accepted)] }]])
    sent = @client.emails.send({})
    assert sent['recipients'][0].key?('id')
    assert_nil sent['recipients'][0]['id']
    assert_equal 'order-1', sent['message_id']
    batch = @client.emails.batch([{}])
    assert batch['results'][0]['recipients'][0].key?('id')
    assert_nil batch['results'][0]['recipients'][0]['id']
    assert_equal 2, @requests.length
  end

  def test_recipient_refusal_field
    @responses << [422, { error: 'Recipient refused', code: 'rejected', field: 'to[1]' }]
    error = assert_raises(CarrierCrow::Error) { @client.emails.send({}) }
    assert_equal 422, error.status
    assert_equal 'rejected', error.code
    assert_equal 'to[1]', error.field
    assert_equal 1, @requests.length
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
      assert_equal 'carriercrow-ruby/0.2.0', headers['user-agent']
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

  def test_missing_key_and_environment_fallback
    saved = ENV.delete('CARRIERCROW_API_KEY')
    error = assert_raises(CarrierCrow::Error) { CarrierCrow::Client.new }
    assert_equal 'missing_api_key', error.code
    error = assert_raises(CarrierCrow::Error) { CarrierCrow::Client.new('') }
    assert_equal 'missing_api_key', error.code
    ENV['CARRIERCROW_API_KEY'] = 'ccm_test_env'
    assert_instance_of CarrierCrow::Client, CarrierCrow::Client.new
  ensure
    saved.nil? ? ENV.delete('CARRIERCROW_API_KEY') : ENV['CARRIERCROW_API_KEY'] = saved
  end

  def test_retry_after_cap
    [429, 503].each do |status|
      [{}, { max_retry_after: 2 }].each do |options|
        cap = options.fetch(:max_retry_after, 60)
        delays = []
        client = CarrierCrow::Client.new('ccm_test', base_url: "http://127.0.0.1:#{@server.addr[1]}",
                                                     sleeper: ->(delay) { delays << delay }, **options)
        @responses.concat([[status, {}, "Retry-After: #{cap}\r\n"], [200, {}]])
        client.domains.list
        assert_equal [cap.to_f], delays
        start = @requests.length
        @responses << [status, { error: 'later', code: 'unavailable', field: 'to' }, "Retry-After: #{cap + 1}\r\n"]
        error = assert_raises(CarrierCrow::Error) { client.domains.list }
        assert_equal [status, 'unavailable', 'to', (cap + 1).to_f],
                     [error.status, error.code, error.field, error.retry_after]
        assert_equal start + 1, @requests.length
        assert_equal 1, delays.length
      end
    end
  end

  def test_paging_rejects_cursor_cycles
    [%w[loop loop], %w[one two one]].each do |cursors|
      @responses.concat(cursors.map { |cursor| [200, { emails: [], next: cursor }] })
      error = assert_raises(CarrierCrow::Error) { @client.emails.each.to_a }
      assert_equal 'invalid_response', error.code
    end
    assert_equal 5, @requests.length
  end

  def test_first_timestamp_wins
    body = JSON.generate(webhook: 'wh_test', delivery: 'del_test', events: [])
    digest = OpenSSL::HMAC.hexdigest('SHA256', 'whsec_test', "100.#{body}")
    assert_equal JSON.parse(body),
                 CarrierCrow::Webhooks.verify(body, "t=100,t=101,v1=#{digest}", 'whsec_test', now: 100)
    ["t=101,t=100,v1=#{digest}", "t=bad,t=100,v1=#{digest}", "t=100,v1=#{digest.upcase}"].each do |header|
      error = assert_raises(CarrierCrow::Error) { CarrierCrow::Webhooks.verify(body, header, 'whsec_test', now: 100) }
      assert_equal 'invalid_signature', error.code
    end
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
