package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

type FeasibilityResult struct {
	Convertible bool   `json:"convertible"`
	Reason      string `json:"reason"`
}

type ToolsResult struct {
	RequiredTools []string `json:"required_tools"`
	Alternatives  []string `json:"alternatives"`
}

type ExecutionStep struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

type ExecutionPlan struct {
	Steps []ExecutionStep `json:"steps"`
}

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
	mu         sync.Mutex
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

// CompleteWithGrammar queries llama.cpp with a GBNF grammar constraint and enforces a 20s timeout and token limit
func (c *Client) CompleteWithGrammar(ctx context.Context, systemPrompt, userPrompt, grammar string, maxTokens int) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Enforce 20-second contextual timeout per inference request to never lock mutex indefinitely
	infCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	if maxTokens <= 0 {
		maxTokens = 256
	}

	// First try llama.cpp native endpoint (/completion) which natively takes grammar
	nativeReqBody := map[string]interface{}{
		"prompt":      fmt.Sprintf("<|im_start|>system\n%s<|im_end|>\n<|im_start|>user\n%s<|im_end|>\n<|im_start|>assistant\n", systemPrompt, userPrompt),
		"grammar":     grammar,
		"temperature": 0.1,
		"n_predict":   maxTokens,
	}

	bodyBytes, err := json.Marshal(nativeReqBody)
	if err == nil {
		endpoint := fmt.Sprintf("%s/completion", c.baseURL)
		req, reqErr := http.NewRequestWithContext(infCtx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
		if reqErr == nil {
			req.Header.Set("Content-Type", "application/json")
			resp, doErr := c.httpClient.Do(req)
			if doErr == nil {
				defer resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					var comp struct {
						Content string `json:"content"`
					}
					if err := json.NewDecoder(resp.Body).Decode(&comp); err == nil && comp.Content != "" {
						return strings.TrimSpace(comp.Content), nil
					}
				}
			}
		}
	}

	// Fallback to OpenAI-compatible /v1/chat/completions with grammar parameter
	chatReqBody := map[string]interface{}{
		"model": c.model,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": userPrompt},
		},
		"grammar":     grammar,
		"temperature": 0.1,
		"max_tokens":  maxTokens,
		"n_predict":   maxTokens,
	}

	chatBytes, err := json.Marshal(chatReqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal chat grammar request: %w", err)
	}

	chatEndpoint := fmt.Sprintf("%s/v1/chat/completions", c.baseURL)
	req, err := http.NewRequestWithContext(infCtx, http.MethodPost, chatEndpoint, bytes.NewReader(chatBytes))
	if err != nil {
		return "", fmt.Errorf("failed to create chat grammar request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("llm grammar request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respData, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("llm grammar response status %d: %s", resp.StatusCode, string(respData))
	}

	var chatCompletion struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&chatCompletion); err != nil {
		return "", fmt.Errorf("failed to decode chat grammar response: %w", err)
	}

	if len(chatCompletion.Choices) == 0 {
		return "", fmt.Errorf("empty choices from llm grammar response")
	}

	return strings.TrimSpace(chatCompletion.Choices[0].Message.Content), nil
}

// CheckFeasibility (Phase 1) checks if the file can be converted to the target format
func (c *Client) CheckFeasibility(ctx context.Context, originalName string, size int64, detectedMime, detectedExt, targetExt string) (*FeasibilityResult, error) {
	systemPrompt := `You are a strict media-type feasibility evaluator.
Rule: A direct file conversion is ONLY feasible (convertible=true) if source and target formats belong to the SAME media category:

Image to Image (e.g. JPG to WEBP, PNG to SVG, TIFF to JPG) -> TRUE
Video to Video (e.g. MKV to MP4, AVI to WEBM) -> TRUE
Video to Audio extraction (e.g. MP4 to MP3, MKV to WAV) -> TRUE
Audio to Audio (e.g. WAV to MP3, FLAC to OGG) -> TRUE
Document to Document (e.g. DOCX to PDF, MD to HTML, TXT to PDF) -> TRUE
Archive to Archive (e.g. TAR to ZIP) -> TRUE

STRICT FORBIDDEN CASES (MUST RETURN convertible=false):
Static Image to Video (e.g. JPG to MP4, PNG to MKV) -> FALSE (a single still image is not a video stream)
Static Image to Audio (e.g. JPG to MP3, PNG to WAV) -> FALSE
Audio to Video (without dedicated visualizer) -> FALSE
Audio to Image -> FALSE

If convertible=false, provide a concise user-friendly reason (e.g. 'Cannot convert a static image into a video file').
Respond strictly using the JSON grammar.`
	userPrompt := fmt.Sprintf(`Source File: %s
File Size: %d bytes
Detected MIME: %s
Source Extension: %s
Target Extension: %s

Can this source format technically be converted or exported to the target extension?`, originalName, size, detectedMime, detectedExt, targetExt)

	raw, err := c.CompleteWithGrammar(ctx, systemPrompt, userPrompt, GrammarFeasibility, 256)
	if err != nil {
		// Fallback to rule engine check
		fb, fbErr := FallbackRuleEngine(originalName, detectedMime, detectedExt, targetExt, nil)
		if fbErr == nil {
			return &FeasibilityResult{
				Convertible: fb.IsConvertible,
				Reason:      "Rule engine deterministic analysis",
			}, nil
		}
		return nil, err
	}

	var res FeasibilityResult
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		return nil, fmt.Errorf("failed to parse feasibility json: %w (raw: %s)", err, raw)
	}
	return &res, nil
}

// DiscoverTools (Phase 2) discovers required CLI tools and possible alternatives
func (c *Client) DiscoverTools(ctx context.Context, sourceExt, targetExt string) (*ToolsResult, error) {
	systemPrompt := `You are an expert system administrator for CLI file conversion. Identify the required CLI tools (binary names) and alternatives to convert the source extension to target extension. Return strictly a JSON object adhering to the grammar.`
	userPrompt := fmt.Sprintf(`Source Extension: %s
Target Extension: %s

What CLI tools are needed to perform this conversion? Examples of binaries: ffmpeg, pandoc, libreoffice, vips, pdftoppm, gs, convert, magick.`, sourceExt, targetExt)

	raw, err := c.CompleteWithGrammar(ctx, systemPrompt, userPrompt, GrammarTools, 256)
	if err != nil {
		// Fallback to rule engine tool
		fb, fbErr := FallbackRuleEngine("input."+sourceExt, "", sourceExt, targetExt, nil)
		if fbErr == nil && fb.RequiredTool != "" {
			return &ToolsResult{
				RequiredTools: []string{fb.RequiredTool},
				Alternatives:  []string{},
			}, nil
		}
		return nil, err
	}

	var res ToolsResult
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		return nil, fmt.Errorf("failed to parse tools json: %w (raw: %s)", err, raw)
	}
	return &res, nil
}

// SynthesizePlan (Phase 3) generates the exact bash command steps
func (c *Client) SynthesizePlan(ctx context.Context, sourceExt, targetExt string, availableTools []string, errorFeedback string) (*ExecutionPlan, error) {
	systemPrompt := `You are an expert command line synthesizer. Generate the exact execution steps to convert a file from source to target. Use $INPUT as placeholder for the source file and $OUTPUT for the destination file. Return strictly a JSON object adhering to the grammar.`
	userPrompt := fmt.Sprintf(`Source Extension: %s
Target Extension: %s
Available CLI tools on system: [%s]
Previous error feedback (if any): %s

Generate steps with command (the binary name) and args array.`, sourceExt, targetExt, strings.Join(availableTools, ", "), errorFeedback)

	raw, err := c.CompleteWithGrammar(ctx, systemPrompt, userPrompt, GrammarExecution, 512)
	if err != nil {
		// Fallback to rule engine
		fb, fbErr := FallbackRuleEngine("input."+sourceExt, "", sourceExt, targetExt, availableTools)
		if fbErr == nil {
			parts := strings.Fields(fb.CommandTemplate)
			if len(parts) > 0 {
				var args []string
				for _, p := range parts[1:] {
					p = strings.ReplaceAll(p, "{{input}}", "$INPUT")
					p = strings.ReplaceAll(p, "{{output}}", "$OUTPUT")
					args = append(args, p)
				}
				return &ExecutionPlan{
					Steps: []ExecutionStep{
						{Command: parts[0], Args: args},
					},
				}, nil
			}
		}
		return nil, err
	}

	var res ExecutionPlan
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		return nil, fmt.Errorf("failed to parse execution plan json: %w (raw: %s)", err, raw)
	}
	return &res, nil
}

// PlanConversion backward compatibility method
func (c *Client) PlanConversion(ctx context.Context, originalName, detectedMime, detectedExt, targetExt string, installedTools []string) (*DecisionResponse, error) {
	feas, err := c.CheckFeasibility(ctx, originalName, 0, detectedMime, detectedExt, targetExt)
	if err != nil || !feas.Convertible {
		reason := "Format conversion not supported"
		if feas != nil && feas.Reason != "" {
			reason = feas.Reason
		}
		return &DecisionResponse{
			DetectedMime:    detectedMime,
			IsConvertible:   false,
			RejectionReason: &reason,
		}, nil
	}

	tools, err := c.DiscoverTools(ctx, detectedExt, targetExt)
	reqTool := "pandoc"
	if err == nil && len(tools.RequiredTools) > 0 {
		reqTool = tools.RequiredTools[0]
	}

	toolInstalled := false
	for _, it := range installedTools {
		if strings.EqualFold(it, reqTool) {
			toolInstalled = true
			break
		}
	}

	plan, err := c.SynthesizePlan(ctx, detectedExt, targetExt, installedTools, "")
	cmdTemplate := fmt.Sprintf("%s {{input}} {{output}}", reqTool)
	if err == nil && len(plan.Steps) > 0 {
		step := plan.Steps[0]
		cmdTemplate = step.Command + " " + strings.Join(step.Args, " ")
		cmdTemplate = strings.ReplaceAll(cmdTemplate, "$INPUT", "{{input}}")
		cmdTemplate = strings.ReplaceAll(cmdTemplate, "$OUTPUT", "{{output}}")
	}

	return &DecisionResponse{
		DetectedMime:    detectedMime,
		IsConvertible:   true,
		RequiredTool:    reqTool,
		ToolInstalled:   toolInstalled,
		CommandTemplate: cmdTemplate,
	}, nil
}

// FallbackRuleEngine provides deterministic templates for offline or fallback conversion
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
	case isImage(src) && isVideo(tgt):
		reason := "Cannot convert a static image into a video file"
		return &DecisionResponse{
			DetectedMime:    detectedMime,
			IsConvertible:   false,
			RejectionReason: &reason,
		}, nil
	case isImage(src) && isAudio(tgt):
		reason := "Cannot convert an image into an audio file"
		return &DecisionResponse{
			DetectedMime:    detectedMime,
			IsConvertible:   false,
			RejectionReason: &reason,
		}, nil
	case isAudio(src) && isVideo(tgt):
		reason := "Cannot convert an audio file into a video file without dedicated visualizer"
		return &DecisionResponse{
			DetectedMime:    detectedMime,
			IsConvertible:   false,
			RejectionReason: &reason,
		}, nil
	case isAudio(src) && isImage(tgt):
		reason := "Cannot convert an audio file into an image"
		return &DecisionResponse{
			DetectedMime:    detectedMime,
			IsConvertible:   false,
			RejectionReason: &reason,
		}, nil
	case isMedia(src) && isMedia(tgt):
		tool = "ffmpeg"
		cmd = "ffmpeg -y -i {{input}} {{output}}"
	case (src == "docx" || src == "doc" || src == "odt" || src == "rtf" || src == "txt") && tgt == "pdf":
		tool = "libreoffice"
		cmd = "libreoffice --headless --convert-to pdf {{input}} --outdir $(dirname {{output}})"
	case (src == "xlsx" || src == "xls" || src == "ods" || src == "csv") && (tgt == "pdf" || tgt == "csv"):
		tool = "libreoffice"
		cmd = fmt.Sprintf("libreoffice --headless --convert-to %s {{input}} --outdir $(dirname {{output}})", tgt)
	case (src == "pptx" || src == "ppt" || src == "odp") && tgt == "pdf":
		tool = "libreoffice"
		cmd = "libreoffice --headless --convert-to pdf {{input}} --outdir $(dirname {{output}})"
	case (src == "md" || src == "markdown" || src == "html" || src == "rst") && (tgt == "html" || tgt == "docx" || tgt == "pdf" || tgt == "md" || tgt == "txt"):
		tool = "pandoc"
		cmd = "pandoc {{input}} -o {{output}}"
	case isImage(src) && isImage(tgt):
		tool = "vips"
		cmd = "vips copy {{input}} {{output}}"
	case src == "pdf" && isImage(tgt):
		tool = "pdftoppm"
		cmd = fmt.Sprintf("pdftoppm -%s -r 150 {{input}} $(dirname {{output}})/page", tgt)
	case isArchive(src) && isArchive(tgt):
		tool = "tar"
		cmd = "tar -czvf {{output}} {{input}}"
	default:
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
	return isVideo(ext) || isAudio(ext)
}

func isVideo(ext string) bool {
	videos := []string{"mp4", "mkv", "avi", "mov", "webm", "flv", "wmv", "m4v"}
	for _, v := range videos {
		if ext == v {
			return true
		}
	}
	return false
}

func isAudio(ext string) bool {
	audios := []string{"mp3", "wav", "flac", "aac", "ogg", "m4a", "wma", "opus"}
	for _, a := range audios {
		if ext == a {
			return true
		}
	}
	return false
}

func isArchive(ext string) bool {
	archives := []string{"tar", "zip", "gz", "bz2", "xz", "7z", "rar"}
	for _, a := range archives {
		if ext == a {
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
