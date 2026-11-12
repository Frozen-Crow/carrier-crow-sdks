require 'net/http'
require 'json'
require 'openssl'
require 'securerandom'
require 'uri'

module CarrierCrow
  VERSION = '0.1.0'
  class Error < StandardError
    attr_reader :status, :code, :field, :retry_after

    def initialize(message, status: nil, code: nil, field: nil, retry_after: nil)
      super(message)
      @status, @code, @field, @retry_after = status, code, field, retry_after
    end
  end

  class Webhooks
    def self.verify(body, signature, secret, tolerance: 300, now: Time.now.to_i)
      parts = signature.to_s.split(',').map { |p| p.strip.split('=', 2) }
      timestamps = parts.select { |k, _| k == 't' }.map(&:last)
      timestamp = timestamps.first
      valid = timestamp && timestamp.match?(/\A\d+\z/) && now.finite? && tolerance.finite? &&
              tolerance >= 0 && (now - timestamp.to_i).abs <= tolerance
      digest = OpenSSL::HMAC.hexdigest('SHA256', secret, "#{timestamp}.".b + body.b)
      valid &&= parts.any? { |k, v|
        k == 'v1' && v && v.match?(/\A[0-9a-f]{64}\z/) && OpenSSL.fixed_length_secure_compare(digest, v)
      }
      raise Error.new('Invalid webhook signature', code: 'invalid_signature') unless valid

      parsed = JSON.parse(body)
      unless parsed.is_a?(Hash) && parsed['webhook'].is_a?(String) &&
             parsed['delivery'].is_a?(String) && parsed['events'].is_a?(Array)
        raise Error.new('Invalid webhook envelope', code: 'invalid_payload')
      end

      parsed
    rescue JSON::ParserError
      raise Error.new('Invalid webhook JSON', code: 'invalid_payload')
    end
  end

  class Client
    attr_reader :emails, :domains, :webhooks

    def initialize(api_key = ENV['CARRIERCROW_API_KEY'],
                   base_url: 'https://api.mta.carriercrow.com', retries: 2, timeout: 30, max_retry_after: 60,
                   sleeper: ->(seconds) { sleep(seconds) })
      raise Error.new('API key is required', code: 'missing_api_key') if api_key.nil? || api_key.empty?
      unless max_retry_after.is_a?(Numeric) && max_retry_after.finite? && max_retry_after >= 0
        raise ArgumentError, 'max_retry_after must be finite and nonnegative'
      end

      @max_retry_after = max_retry_after
      raise ArgumentError, 'retries must be a nonnegative integer' unless retries.is_a?(Integer) && retries >= 0

      @api_key, @base_url, @retries, @timeout, @sleeper = api_key, base_url.sub(%r{/+\z}, ''), retries, timeout, sleeper
      @emails, @domains, @webhooks = Emails.new(self), Domains.new(self), Webhooks
    end

    def request(method, path, query: {}, body: nil, idempotency_key: nil, retries: nil, send_request: false)
      count = retries.nil? ? @retries : retries
      raise ArgumentError, 'retries must be a nonnegative integer' unless count.is_a?(Integer) && count >= 0
      if idempotency_key && (!idempotency_key.is_a?(String) || !idempotency_key.length.between?(1, 255))
        raise ArgumentError, 'idempotency_key must contain 1–255 characters'
      end

      idempotency_key ||= SecureRandom.uuid if send_request && !retries.nil? && retries > 0
      count = 0 if send_request && !idempotency_key
      uri = URI(@base_url + path)
      uri.query = URI.encode_www_form(query.flat_map { |k, v| Array(v).map { |value| [k, value] } }) unless query.empty?
      encoded = body.nil? ? nil : JSON.generate(body)
      attempt = 0
      loop do
        begin
          request = Net::HTTP.const_get(method.capitalize).new(uri)
          request['Authorization'] = "Bearer #{@api_key}"
          request['User-Agent'] = "carriercrow-ruby/#{VERSION}"
          request['Accept'] = 'application/json'
          request['Idempotency-Key'] = idempotency_key if idempotency_key
          request['Content-Type'] = 'application/json' if encoded
          request.body = encoded
          http = Net::HTTP.new(uri.host, uri.port)
          http.use_ssl = uri.scheme == 'https'
          http.open_timeout = http.read_timeout = @timeout
          http.max_retries = 0
          response = http.start { |connection| connection.request(request) }
          status = response.code.to_i
          data = begin
            JSON.parse(response.body.to_s)
          rescue JSON::ParserError
            nil
          end
          if status.between?(200, 299)
            unless data.is_a?(Hash)
              raise Error.new('Invalid JSON response', status: status, code: 'invalid_response')
            end

            return data
          end
          retry_after = Float(response['Retry-After'], exception: false)
          retry_after = nil unless retry_after && retry_after.finite? && retry_after >= 0
          data = {} unless data.is_a?(Hash)
          error = Error.new(data['error'] || "HTTP #{status}", status: status, code: data['code'],
                                                               field: data['field'], retry_after: retry_after)
          within_cap = retry_after.nil? || retry_after <= @max_retry_after
          raise error unless [429, 503].include?(status) && attempt < count && within_cap
        rescue IOError, SystemCallError, Timeout::Error, SocketError, OpenSSL::SSL::SSLError => cause
          error = Error.new(cause.message, code: 'network_error')
          raise error unless attempt < count

          retry_after = nil
        end
        @sleeper.call([retry_after || 0, 0.25 * (2**attempt) * (0.5 + rand)].max)
        attempt += 1
      end
    end
  end

  class Emails
    def initialize(client)
      @client = client
    end

    def send(params, **options)
      @client.request('post', '/v1/emails', body: params, send_request: true, **options)
    end

    def batch(emails, **options)
      @client.request('post', '/v1/emails/batch', body: { emails: emails }, send_request: true, **options)
    end

    def get(id)
      @client.request('get', "/v1/emails/#{URI.encode_www_form_component(id).gsub('+', '%20')}")
    end

    def list(query = {})
      @client.request('get', '/v1/emails', query: query)
    end

    def each(query = {})
      return enum_for(:each, query) unless block_given?

      query = query.transform_keys(&:to_s)
      seen = [query['before']].compact
      loop do
        page = list(query)
        page.fetch('emails').each { |email| yield email }
        break if page['next'].nil? || page['next'].empty?

        raise Error.new('Repeated pagination cursor', code: 'invalid_response') if seen.include?(page['next'])

        seen << page['next']
        query = query.merge('before' => page['next'])
      end
    end
  end

  class Domains
    def initialize(client)
      @client = client
    end

    def list
      @client.request('get', '/v1/domains')
    end

    def get(id)
      @client.request('get', "/v1/domains/#{URI.encode_www_form_component(id).gsub('+', '%20')}")
    end

    def check(id)
      @client.request('post', "/v1/domains/#{URI.encode_www_form_component(id).gsub('+', '%20')}/check")
    end
  end
end
