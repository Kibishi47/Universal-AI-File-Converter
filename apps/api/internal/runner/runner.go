package runner

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
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
	// Take only the first word (the executable binary)
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

// ExecuteConversion runs the CLI command with a strict timeout (120s)
func (r *Runner) ExecuteConversion(ctx context.Context, commandTemplate, inputPath, outputPath string) (string, error) {
	// Interpolate {{input}} and {{output}}
	cmdStr := strings.ReplaceAll(commandTemplate, "{{input}}", fmt.Sprintf("%q", inputPath))
	cmdStr = strings.ReplaceAll(cmdStr, "{{output}}", fmt.Sprintf("%q", outputPath))

	// Timeout context
	execCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd := exec.CommandContext(execCtx, "sh", "-c", cmdStr)
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	err := cmd.Run()
	combinedLogs := fmt.Sprintf("STDOUT:\n%s\nSTDERR:\n%s", stdoutBuf.String(), stderrBuf.String())

	if execCtx.Err() == context.DeadlineExceeded {
		return combinedLogs, fmt.Errorf("conversion timed out after 120 seconds")
	}

	if err != nil {
		return combinedLogs, fmt.Errorf("conversion command failed: %w", err)
	}

	return combinedLogs, nil
}
