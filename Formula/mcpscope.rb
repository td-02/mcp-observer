class Mcpscope < Formula
  desc "Transparent MCP proxy with tracing, schema snapshots, and a live dashboard"
  homepage "https://github.com/td-02/mcpscope"
  url "https://github.com/td-02/mcpscope/releases/download/v0.1.0/mcpscope_REPLACE_ME.tar.gz"
  sha256 "REPLACE_WITH_RELEASE_ARCHIVE_SHA256"
  license "MIT"

  # After each release:
  # 1. Update the version in the URL.
  # 2. Point the URL at the correct archive for the target platform.
  # 3. Replace the SHA256 placeholder with the value from checksums.txt.

  def install
    bin.install "mcpscope"
  end

  test do
    system "#{bin}/mcpscope", "--help"
  end
end
