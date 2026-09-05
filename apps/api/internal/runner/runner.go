package runner

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	MaxLogBufferBytes = 2 * 1024 * 1024 // 2 MB limit to prevent memory exhaustion
	ExecutionTimeout  = 120 * time.Second
)

type Runner struct {
	pkgManager string // "apk" or "apt-get"
}

func New() *Runner {
	pm := ""
	if _, err := exec.LookPath("apk"); err == nil {
		pm = "apk"
	} else if _, err := exec.LookPath("apt-get"); err == nil {
		pm = "apt-get"
	}

	return &Runner{
		pkgManager: pm,
	}
}

// CheckTool checks if the specified binary is in PATH
func (r *Runner) CheckTool(toolName string) bool {
	if toolName == "" {
		return false
	}
	parts := strings.Fields(toolName)
	if len(parts) == 0 {
		return false
	}
	_, err := exec.LookPath(parts[0])
	return err == nil
}

// ListInstalledTools returns a list of common conversion tools currently present
func (r *Runner) ListInstalledTools() []string {
	toolsToCheck := []string{
		"ffmpeg", "pandoc", "libreoffice", "soffice", "vips",
		"pdftoppm", "pdftotext", "pdf2ps", "gs", "convert", "magick",
		"graphviz", "dot", "tesseract", "sox", "inkscape",
	}

	var installed []string
	for _, tool := range toolsToCheck {
		if r.CheckTool(tool) {
			installed = append(installed, tool)
		}
	}
	return installed
}

// InstallTool attempts to install the missing tool dynamically without stopping the container
func (r *Runner) InstallTool(ctx context.Context, toolName string) error {
	if r.pkgManager == "" {
		return fmt.Errorf("no supported package manager found (neither apk nor apt-get)")
	}

	var cmd *exec.Cmd
	switch r.pkgManager {
	case "apk":
		cmd = exec.CommandContext(ctx, "apk", "add", "--no-cache", toolName)
	case "apt-get":
		cmd = exec.CommandContext(ctx, "sh", "-c", fmt.Sprintf("apt-get update && apt-get install -y --no-install-recommends %s", toolName))
	}

	cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("dynamic installation failed: %w (output: %s)", err, string(output))
	}

	return nil
}

// ValidateCommand enforces security checks against prohibited or destructive commands
func (r *Runner) ValidateCommand(command string, args []string, allowedDir string) error {
	cmdLower := strings.ToLower(filepath.Base(command))

	// Prohibited dangerous tools or privilege escalation
	blockedCommands := []string{"sudo", "su", "chown", "chmod", "curl", "wget", "nc", "ncat", "netcat", "mkfs", "dd", "shutdown", "reboot", "init"}
	for _, blocked := range blockedCommands {
		if cmdLower == blocked {
			return fmt.Errorf("prohibited command: %s is not permitted in sandbox", command)
		}
	}

	// Restrict 'rm' to session directory only
	if cmdLower == "rm" {
		cleanAllowed := filepath.Clean(allowedDir)
		for _, arg := range args {
			if strings.HasPrefix(arg, "-") {
				continue
			}
			cleanArg := filepath.Clean(arg)
			if !strings.HasPrefix(cleanArg, cleanAllowed) {
				return fmt.Errorf("safety violation: rm is restricted to session directory %s", cleanAllowed)
			}
		}
	}

	return nil
}

// LimitedBuffer caps memory usage to MaxLogBufferBytes
type LimitedBuffer struct {
	buf   bytes.Buffer
	limit int
}

func NewLimitedBuffer(limit int) *LimitedBuffer {
	return &LimitedBuffer{limit: limit}
}

func (b *LimitedBuffer) Write(p []byte) (n int, err error) {
	currentLen := b.buf.Len()
	if currentLen >= b.limit {
		return len(p), nil // drop silently without failing execution
	}
	remaining := b.limit - currentLen
	if len(p) > remaining {
		b.buf.Write(p[:remaining])
		b.buf.WriteString("\n[...log output truncated due to 2MB buffer limit...]\n")
		return len(p), nil
	}
	return b.buf.Write(p)
}

func (b *LimitedBuffer) String() string {
	return b.buf.String()
}

// ExecuteStep runs an individual command with args within a sandbox directory with strict timeout
func (r *Runner) ExecuteStep(ctx context.Context, command string, args []string, workingDir string) (string, string, error) {
	if err := r.ValidateCommand(command, args, workingDir); err != nil {
		return "", "", err
	}

	execCtx, cancel := context.WithTimeout(ctx, ExecutionTimeout)
	defer cancel()

	cmd := exec.CommandContext(execCtx, command, args...)
	cmd.Dir = workingDir

	stdoutBuf := NewLimitedBuffer(MaxLogBufferBytes)
	stderrBuf := NewLimitedBuffer(MaxLogBufferBytes)
	cmd.Stdout = stdoutBuf
	cmd.Stderr = stderrBuf

	err := cmd.Run()

	if execCtx.Err() == context.DeadlineExceeded {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		return stdoutBuf.String(), stderrBuf.String(), fmt.Errorf("command execution timed out after %v", ExecutionTimeout)
	}

	return stdoutBuf.String(), stderrBuf.String(), err
}

// ExecuteConversion provides fallback execution for command templates with {{input}} and {{output}}
func (r *Runner) ExecuteConversion(ctx context.Context, commandTemplate, inputPath, outputPath string) (string, error) {
	cmdStr := strings.ReplaceAll(commandTemplate, "{{input}}", fmt.Sprintf("%q", inputPath))
	cmdStr = strings.ReplaceAll(cmdStr, "{{output}}", fmt.Sprintf("%q", outputPath))

	execCtx, cancel := context.WithTimeout(ctx, ExecutionTimeout)
	defer cancel()

	stdoutBuf := NewLimitedBuffer(MaxLogBufferBytes)
	stderrBuf := NewLimitedBuffer(MaxLogBufferBytes)

	cmd := exec.CommandContext(execCtx, "sh", "-c", cmdStr)
	cmd.Dir = filepath.Dir(inputPath)
	cmd.Stdout = stdoutBuf
	cmd.Stderr = stderrBuf

	err := cmd.Run()
	combinedLogs := fmt.Sprintf("STDOUT:\n%s\nSTDERR:\n%s", stdoutBuf.String(), stderrBuf.String())

	if execCtx.Err() == context.DeadlineExceeded {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		return combinedLogs, fmt.Errorf("conversion timed out after %v", ExecutionTimeout)
	}

	if err != nil {
		return combinedLogs, fmt.Errorf("conversion command failed: %w", err)
	}

	return combinedLogs, nil
}
