package commands

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/spf13/cobra"

	"github.com/linkanalabs/cli/internal/auth"
	"github.com/linkanalabs/cli/internal/client"
	"github.com/linkanalabs/cli/internal/config"
	"github.com/linkanalabs/cli/internal/mode"
	"github.com/linkanalabs/cli/internal/output"
)

// Check statuses.
const (
	StatusPass = "pass"
	StatusWarn = "warn"
	StatusFail = "fail"
	StatusSkip = "skip"
)

// Check is a single diagnostic result.
type Check struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
}

// Result aggregates diagnostic checks.
type Result struct {
	Checks  []Check `json:"checks"`
	Passed  int     `json:"passed"`
	Failed  int     `json:"failed"`
	Warned  int     `json:"warned"`
	Skipped int     `json:"skipped"`
}

func (r *Result) add(c Check) {
	r.Checks = append(r.Checks, c)
	switch c.Status {
	case StatusPass:
		r.Passed++
	case StatusWarn:
		r.Warned++
	case StatusFail:
		r.Failed++
	case StatusSkip:
		r.Skipped++
	}
}

// Summary returns a one-line summary of the result.
func (r *Result) Summary() string {
	if r.Failed == 0 && r.Warned == 0 {
		return fmt.Sprintf("All %d checks passed", r.Passed)
	}
	return fmt.Sprintf("%d passed, %d warned, %d failed", r.Passed, r.Warned, r.Failed)
}

// Styled renders the result as a checklist.
func (r *Result) Styled() string {
	out := ""
	for _, c := range r.Checks {
		out += fmt.Sprintf("%s %s: %s\n", statusIcon(c.Status), c.Name, c.Message)
		if c.Hint != "" {
			out += "    ↳ " + c.Hint + "\n"
		}
	}
	return out + "\n" + r.Summary() + "\n"
}

func statusIcon(status string) string {
	switch status {
	case StatusPass:
		return "✓"
	case StatusWarn:
		return "!"
	case StatusSkip:
		return "-"
	default:
		return "✗"
	}
}

// doctorInput carries the dependencies a doctor run needs, so the check logic
// stays pure and testable.
type doctorInput struct {
	version    string
	goVersion  string
	os         string
	arch       string
	configPath string
	configErr  error
	baseURL    string
	configDir  string
	httpClient *http.Client

	// Auth check inputs.
	hasToken bool
	identity func(context.Context) (*client.Identity, error)
}

// authCheckInput carries what the auth check needs, decoupled from the client.
type authCheckInput struct {
	reachable bool
	hasToken  bool
	identity  func(context.Context) (*client.Identity, error)
}

// runDoctorChecks runs the diagnostic checks. The Authentication check runs
// after reachability and skips when the backend was unreachable.
func runDoctorChecks(ctx context.Context, in doctorInput) *Result {
	r := &Result{}
	r.add(checkVersion(in.version))
	r.add(checkRuntime(in.goVersion, in.os, in.arch))
	r.add(checkConfig(in.configPath, in.configErr, in.baseURL))
	r.add(checkFilesystem(in.configDir))

	reach := checkReachability(ctx, in.httpClient, in.baseURL)
	r.add(reach)

	r.add(checkAuth(ctx, authCheckInput{
		reachable: reach.Status != StatusFail,
		hasToken:  in.hasToken,
		identity:  in.identity,
	}))
	return r
}

func checkAuth(ctx context.Context, in authCheckInput) Check {
	const name = "Authentication"
	switch {
	case !in.reachable:
		return Check{Name: name, Status: StatusSkip, Message: "Skipped (backend unreachable)"}
	case !in.hasToken:
		return Check{Name: name, Status: StatusSkip, Message: "No token configured", Hint: "Run `lk auth login`"}
	}

	id, err := in.identity(ctx)
	switch {
	case err == nil:
		return Check{Name: name, Status: StatusPass, Message: "Token accepted (" + id.Email + ")"}
	case errors.Is(err, client.ErrUnauthorized):
		return Check{Name: name, Status: StatusFail, Message: "Token rejected (401)", Hint: "Run `lk auth login` to re-authenticate"}
	default:
		return Check{Name: name, Status: StatusFail, Message: "Identity check failed", Hint: err.Error()}
	}
}

func checkVersion(version string) Check {
	return Check{Name: "Version", Status: StatusPass, Message: version}
}

func checkRuntime(goVersion, goos, arch string) Check {
	return Check{
		Name:    "Runtime",
		Status:  StatusPass,
		Message: fmt.Sprintf("%s %s/%s", goVersion, goos, arch),
	}
}

func checkConfig(path string, loadErr error, baseURL string) Check {
	if loadErr != nil {
		return Check{Name: "Config", Status: StatusFail, Message: loadErr.Error(), Hint: "Check " + path}
	}
	if baseURL == "" {
		return Check{Name: "Config", Status: StatusWarn, Message: "No base URL configured", Hint: "Set " + config.EnvBaseURL}
	}
	return Check{Name: "Config", Status: StatusPass, Message: baseURL}
}

func checkFilesystem(dir string) Check {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Check{Name: "Filesystem", Status: StatusFail, Message: "Config dir not writable", Hint: err.Error()}
	}
	probe := filepath.Join(dir, ".lk-write-probe")
	if err := os.WriteFile(probe, []byte("ok"), 0o600); err != nil {
		return Check{Name: "Filesystem", Status: StatusFail, Message: "Config dir not writable", Hint: err.Error()}
	}
	_ = os.Remove(probe)
	return Check{Name: "Filesystem", Status: StatusPass, Message: dir}
}

func checkReachability(ctx context.Context, hc *http.Client, baseURL string) Check {
	const name = "API Reachability"
	if baseURL == "" {
		return Check{Name: name, Status: StatusFail, Message: "No base URL configured"}
	}
	url := baseURL + "/up"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Check{Name: name, Status: StatusFail, Message: "Cannot build request", Hint: err.Error()}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return Check{Name: name, Status: StatusFail, Message: "Cannot reach " + url, Hint: err.Error()}
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		return Check{Name: name, Status: StatusPass, Message: fmt.Sprintf("%s (%d)", url, resp.StatusCode)}
	}
	return Check{
		Name:    name,
		Status:  StatusWarn,
		Message: fmt.Sprintf("%s returned %d", url, resp.StatusCode),
		Hint:    "Backend reachable but not healthy",
	}
}

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Run basic health checks",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, cfgErr := config.Load()
			path, _ := config.Path()
			dir, _ := config.Dir()

			in := doctorInput{
				version:    version,
				goVersion:  runtime.Version(),
				os:         runtime.GOOS,
				arch:       runtime.GOARCH,
				configPath: path,
				configErr:  cfgErr,
				configDir:  dir,
				httpClient: &http.Client{Timeout: 5 * time.Second},
			}
			if cfg != nil {
				in.baseURL = cfg.BaseURL
				if token, _, err := auth.Load(cfg.BaseURL); err == nil && token != "" {
					in.hasToken = true
					m, _ := mode.Load(cfg.BaseURL)
					api := newAPI(cfg.BaseURL, token, m)
					in.identity = api.GetIdentity
				}
			}

			res := runDoctorChecks(cmd.Context(), in)
			if err := output.Render(cmd.OutOrStdout(), formatFlag(cmd), res); err != nil {
				return err
			}
			if res.Failed > 0 {
				return errSilent
			}
			return nil
		},
	}
}
