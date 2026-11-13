Gem::Specification.new do |s|
  s.name = 'carriercrow'
  s.version = '0.1.0'
  s.summary = 'Carrier Crow email API client'
  s.description = 'Send email through Carrier Crow, read what happened to it, check your domains and verify webhook signatures.'
  s.homepage = 'https://github.com/Frozen-Crow/carrier-crow-sdks/tree/main/ruby'
  s.metadata = { 'source_code_uri' => 'https://github.com/Frozen-Crow/carrier-crow-sdks', 'bug_tracker_uri' => 'https://github.com/Frozen-Crow/carrier-crow-sdks/issues', 'rubygems_mfa_required' => 'true' }
  s.authors = ['Frozen Crow Inc.']
  s.license = 'MIT'
  s.files = Dir['lib/**/*.rb', 'README.md', 'LICENSE']
  s.required_ruby_version = '>= 3.1'
end
