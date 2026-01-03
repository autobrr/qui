class Qui < Formula
  desc "Modern, self-hosted web interface for managing multiple qBittorrent instances"
  homepage "https://github.com/autobrr/qui"
  url "https://github.com/autobrr/qui/archive/refs/tags/v1.0.0.tar.gz"
  sha256 "PLACEHOLDER_SHA256"
  license "GPL-2.0-or-later"
  head "https://github.com/autobrr/qui.git", branch: "main"

  depends_on "go" => :build
  depends_on "node" => :build
  depends_on "pnpm" => :build

  def install
    system "pnpm", "install", "--dir", "web"
    system "pnpm", "--dir", "web", "run", "build"

    # Copy built frontend to internal/web/dist for embedding
    rm_rf "internal/web/dist"
    cp_r "web/dist", "internal/web/"

    ldflags = "-s -w -X github.com/autobrr/qui/internal/buildinfo.Version=#{version} -X github.com/autobrr/qui/internal/buildinfo.Commit=#{tap.user}"

    system "go", "build", *std_go_args(output: bin/"qui", ldflags:), "./cmd/qui"

    (var/"qui").mkpath
  end

  def post_install
    (var/"qui").mkpath
  end

  service do
    run [opt_bin/"qui", "serve", "--config-dir", var/"qui/"]
    keep_alive true
    log_path var/"log/qui.log"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/qui version")

    port = free_port

    (testpath/"config.toml").write <<~TOML
      host = "127.0.0.1"
      port = #{port}
      logLevel = "INFO"
      checkForUpdates = false
      sessionSecret = "test-secret-key-for-homebrew-testing"
    TOML

    pid = spawn bin/"qui", "serve", "--config-dir", "#{testpath}/"
    begin
      sleep 4
      system "curl", "-s", "--fail", "http://127.0.0.1:#{port}/api/healthz/liveness"
    ensure
      Process.kill("TERM", pid)
      Process.wait(pid)
    end
  end
end
