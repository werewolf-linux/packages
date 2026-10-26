# Exercise Mastodon's real loader policy against the installed media libraries.
ENV['BUNDLE_GEMFILE'] = '/usr/lib/mastodon/Gemfile'
require 'bundler/setup'
require '/usr/lib/mastodon/config/initializers/vips'
require 'base64'
require 'tmpdir'
raise 'optional OTel SDK remains' if Gem::Specification.any? { |g| g.name == 'opentelemetry-sdk' }
Dir.mktmpdir do |dir|
  source = Vips::Image.black(32, 24, bands: 3)
  %w[jpg png webp].each do |ext|
    input = "#{dir}/input.#{ext}"
    source.write_to_file(input)
    thumb = Vips::Image.thumbnail(input, 16)
    raise 'incorrect thumbnail' unless thumb.width == 16 && thumb.height == 12
    thumb.write_to_file("#{dir}/thumb-#{ext}.png")
  end
  gif = "#{dir}/input.gif"
  File.binwrite(gif, Base64.decode64('R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7'))
  raise 'GIF decode failed' unless Vips::Image.new_from_file(gif).width == 1
end
puts 'PASS: Mastodon loader policy, retained image formats and omitted OTel exporters'
