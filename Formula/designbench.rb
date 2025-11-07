class Designbench < Formula
  desc "Cross-platform UI benchmark runner for Kotlin Multiplatform apps"
  homepage "https://github.com/tahatesser/designbench"
  head "https://github.com/tahatesser/designbench.git", branch: "main"

  depends_on "go" => :build

  def install
    system "go", "build", *std_go_args(output: bin/"designbench"), "./cmd/designbench"
  end

  test do
    assert_match "DesignBench", shell_output("#{bin}/designbench --help")
  end
end
