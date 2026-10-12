package rules

import (
    "context"
    "crypto/tls"
    "net/http"
    "os/exec"
    "time"
)

func examples(ctx context.Context, input string) {
    // ruleid: forcad-go-disabled-tls
    _ = tls.Config{InsecureSkipVerify: true}
    // ok: forcad-go-disabled-tls
    _ = tls.Config{MinVersion: tls.VersionTLS13}
    // ruleid: forcad-go-shell-command
    _ = exec.CommandContext(ctx, "sh", "-c", input)
    // ruleid: forcad-go-shell-command
    _ = exec.Command("bash", "-c", input)
    // ok: forcad-go-shell-command
    _ = exec.CommandContext(ctx, "checker", "check", input)
    // ruleid: forcad-go-unbounded-http-client
    _, _ = http.Get(input)
    request, _ := http.NewRequestWithContext(ctx, http.MethodGet, input, nil)
    client := &http.Client{Timeout: 3 * time.Second}
    // ok: forcad-go-unbounded-http-client
    _, _ = client.Do(request)
}
