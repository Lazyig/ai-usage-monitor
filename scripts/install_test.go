package scripts

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestUnixInstallerVerifiesBeforeReplacing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix installer; PowerShell has a separate test")
	}
	for _, fallback := range []bool{false, true} {
		for _, kind := range []string{"valid", "missing", "short", "mismatch"} {
			t.Run(fmt.Sprintf("fallback=%v/%s", fallback, kind), func(t *testing.T) {
				binary := []byte("#!/bin/sh\nprintf 'fixture-version\\n'\n")
				sha := fmt.Sprintf("%x", sha256.Sum256(binary))
				switch kind {
				case "missing":
					sha = ""
				case "short":
					sha = "deadbeef"
				case "mismatch":
					sha = strings.Repeat("a", 64)
				}
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/downloads/manifest.json" {
						fmt.Fprintf(w, `{"files":[{"name":"ai-usage-agent_%s_%s","sha256":"%s"}]}`, runtime.GOOS, runtime.GOARCH, sha)
						return
					}
					w.Write(binary)
				}))
				defer srv.Close()
				dir := t.TempDir()
				script := filepath.Join(dir, "install.sh")
				body := unixInstaller
				if fallback {
					body = UnixInstaller(srv.URL)
				}
				if err := os.WriteFile(script, []byte(body), 0o600); err != nil {
					t.Fatal(err)
				}
				prefix := filepath.Join(dir, "bin")
				if err := os.Mkdir(prefix, 0o700); err != nil {
					t.Fatal(err)
				}
				dest := filepath.Join(prefix, "ai-usage-agent")
				old := []byte("#!/bin/sh\nexit 0\n")
				if err := os.WriteFile(dest, old, 0o755); err != nil {
					t.Fatal(err)
				}
				args := []string{script, "--prefix", prefix, "--no-service"}
				if !fallback {
					args = append(args, "--server", srv.URL)
				}
				cmd := exec.Command("sh", args...)
				if fallback && kind == "valid" {
					// Exercise the fallback on a machine with wget but no curl in
					// PATH. The mock delegates the actual transfer to curl by its
					// absolute path, without changing the installer's selection.
					tools := filepath.Join(dir, "tools")
					if err := os.Mkdir(tools, 0o700); err != nil {
						t.Fatal(err)
					}
					for _, name := range []string{"uname", "tr", "mkdir", "mktemp", "rm", "sed", "cut", "sha256sum", "shasum", "install", "id"} {
						if path, err := exec.LookPath(name); err == nil {
							if err := os.Symlink(path, filepath.Join(tools, name)); err != nil {
								t.Fatal(err)
							}
						}
					}
					wget := "#!/bin/sh\n[ \"$1\" = -qO ] || exit 2\nexec \"$REVIEW_CURL\" -fsSL \"$3\" -o \"$2\"\n"
					if err := os.WriteFile(filepath.Join(tools, "wget"), []byte(wget), 0o755); err != nil {
						t.Fatal(err)
					}
					curl, err := exec.LookPath("curl")
					if err != nil {
						t.Fatal(err)
					}
					cmd.Env = append(os.Environ(), "PATH="+tools, "REVIEW_CURL="+curl)
				}
				output, err := cmd.CombinedOutput()
				got, readErr := os.ReadFile(dest)
				if readErr != nil {
					t.Fatal(readErr)
				}
				if kind == "valid" {
					if err != nil || string(got) != string(binary) {
						t.Fatalf("verified install failed: %v %s", err, output)
					}
				} else if err == nil || string(got) != string(old) {
					t.Fatalf("unverified download replaced the agent: %v %s", err, output)
				}
			})
		}
	}
}

func TestFallbackInstallerQuotesItsDefaultURL(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires sh")
	}
	sentinel := filepath.Join(t.TempDir(), "must-not-exist")
	script := UnixInstaller("https://example.test/'$(touch " + sentinel + ")")
	cmd := exec.Command("sh", "-s", "--", "--help")
	cmd.Stdin = strings.NewReader(script)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("quoted URL broke the installer: %v %s", err, out)
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatal("default URL was evaluated as shell code")
	}
}
