package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type DecisionResponse struct {
	DetectedMime    string  `json:"detected_mime"`
	IsConvertible   bool    `json:"is_convertible"`
	RejectionReason *string `json:"rejection_reason"`
	RequiredTool    string  `json:"required_tool"`
	ToolInstalled   bool    `json:"tool_installed"`
	CommandTemplate string  `json:"command_template"`
}

type Client struct {
	baseURL    string
	httpClient *http.Client
	model      string
}

func NewClient(baseURL, model string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
		model: model,
	}
}

// PlanConversion asks llama.cpp:server using json_schema in response_format to generate a guaranteed JSON response
func (c *Client) PlanConversion(ctx context.Context, originalName, detectedMime, detectedExt, targetExt string, installedTools []string) (*DecisionResponse, error) {
	systemPrompt := `You are an expert file conversion CLI orchestrator.
Analyze the source file metadata and target extension.
Determine whether a technical conversion is feasible.
You must choose the best CLI tool to convert the file from source format to target format.
Use '{{input}}' and '{{output}}' as placeholders in the command_template.
If a tool is present in the list of installed tools, mark tool_installed as true, otherwise false.
Output strictly valid JSON matching the schema provided.`

	userPrompt := fmt.Sprintf(`Source File: %s
Detected MIME: %s
Source Extension: %s
Target Extension: %s
Installed Tools in Container: [%s]

Determine technical feasibility, required tool (e.g. ffmpeg, pandoc, libreoffice, vips, pdftoppm, gs, convert/imagemagick, etc.), check if it is in Installed Tools, and generate the exact CLI command template using {{input}} and {{output}}.`,
		originalName, detectedMime, detectedExt, targetExt, strings.Join(installedTools, ", "))

	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"detected_mime": map[string]interface{}{
				"type": "string",
			},
			"is_convertible": map[string]interface{}{
				"type": "boolean",
			},
			"rejection_reason": map[string]interface{}{
				"type": []string{"string", "null"},
			},
			"required_tool": map[string]interface{}{
				"type": "string",
			},
			"tool_installed": map[string]interface{}{
				"type": "boolean",
			},
			"command_template": map[string]interface{}{
				"type": "string",
			},
		},
		"required": []string{"detected_mime", "is_convertible", "rejection_reason", "required_tool", "tool_installed", "command_template"},
		"additionalProperties": false,
	}

	reqBody := map[string]interface{}{
		"model": c.model,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": userPrompt},
		},
		"temperature": 0.1,
		"response_format": map[string]interface{}{
			"type": "json_object",
			"schema": schema,
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal llm request: %w", err)
	}

	endpoint := fmt.Sprintf("%s/v1/chat/completions", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create llm request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		// Fallback to deterministic rule engine if LLM server is unreachable
		return FallbackRuleEngine(originalName, detectedMime, detectedExt, targetExt, installedTools)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respData, _ := io.ReadAll(resp.Body)
		// Fallback if LLM server returned error (e.g. model still loading)
		fallback, fbErr := FallbackRuleEngine(originalName, detectedMime, detectedExt, targetExt, installedTools)
		if fbErr == nil {
			return fallback, nil
		}
		return nil, fmt.Errorf("llm request failed with status %d: %s", resp.StatusCode, string(respData))
	}

	var chatCompletion struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&chatCompletion); err != nil {
		return nil, fmt.Errorf("failed to decode llm response: %w", err)
	}

	if len(chatCompletion.Choices) == 0 {
		return nil, fmt.Errorf("no completion choices returned by llm")
	}

	content := strings.TrimSpace(chatCompletion.Choices[0].Message.Content)
	var decision DecisionResponse
	if err := json.Unmarshal([]byte(content), &decision); err != nil {
		return nil, fmt.Errorf("failed to parse structured decision JSON: %w (content: %s)", err, content)
	}

	return &decision, nil
}

// FallbackRuleEngine provides high-reliability deterministic CLI templates for common conversions
func FallbackRuleEngine(originalName, detectedMime, detectedExt, targetExt string, installedTools []string) (*DecisionResponse, error) {
	src := strings.ToLower(detectedExt)
	tgt := strings.ToLower(targetExt)

	isInstalled := func(tool string) bool {
		for _, t := range installedTools {
			if strings.EqualFold(t, tool) {
				return true
			}
		}
		return false
	}

	var tool string
	var cmd string

	switch {
	// Audio / Video conversions
	case isMedia(src) && isMedia(tgt):
		tool = "ffmpeg"
		cmd = "ffmpeg -y -i {{input}} {{output}}"
	// Documents (Office / PDF / Text)
	case (src == "docx" || src == "doc" || src == "odt" || src == "rtf" || src == "txt") && tgt == "pdf":
		tool = "libreoffice"
		cmd = "libreoffice --headless --convert-to pdf {{input}} --outdir $(dirname {{output}})"
	case (src == "xlsx" || src == "xls" || src == "ods" || src == "csv") && (tgt == "pdf" || tgt == "csv"):
		tool = "libreoffice"
		cmd = fmt.Sprintf("libreoffice --headless --convert-to %s {{input}} --outdir $(dirname {{output}})", tgt)
	case (src == "pptx" || src == "ppt" || src == "odp") && tgt == "pdf":
		tool = "libreoffice"
		cmd = "libreoffice --headless --convert-to pdf {{input}} --outdir $(dirname {{output}})"
	// Pandoc documents / markdown
	case (src == "md" || src == "markdown" || src == "html" || src == "rst") && (tgt == "html" || tgt == "docx" || tgt == "pdf" || tgt == "md" || tgt == "txt"):
		tool = "pandoc"
		cmd = "pandoc {{input}} -o {{output}}"
	// Images (vips or imagemagick)
	case isImage(src) && isImage(tgt):
		tool = "vips"
		cmd = "vips copy {{input}} {{output}}"
	// PDF to image
	case src == "pdf" && isImage(tgt):
		tool = "pdftoppm"
		cmd = fmt.Sprintf("pdftoppm -%s -r 150 {{input}} $(dirname {{output}})/page", tgt)
	default:
		// Attempt pandoc or ffmpeg or fail
		tool = "pandoc"
		cmd = "pandoc {{input}} -o {{output}}"
	}

	return &DecisionResponse{
		DetectedMime:    detectedMime,
		IsConvertible:   true,
		RejectionReason: nil,
		RequiredTool:    tool,
		ToolInstalled:   isInstalled(tool),
		CommandTemplate: cmd,
	}, nil
}

func isMedia(ext string) bool {
	media := []string{"mp4", "mkv", "avi", "mov", "webm", "mp3", "wav", "flac", "aac", "ogg", "m4a"}
	for _, m := range media {
		if ext == m {
			return true
		}
	}
	return false
}

func isImage(ext string) bool {
	images := []string{"png", "jpg", "jpeg", "webp", "gif", "tiff", "svg", "bmp", "avif"}
	for _, img := range images {
		if ext == img {
			return true
		}
	}
	return false
}
