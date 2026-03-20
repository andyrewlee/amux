package cli

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
)

const assistantFormatCaptureUsage = "Usage: amux assistant format-capture [--strip-ansi] [--last-answer] [--trim]"

var assistantLastAnswerPromptRegex = regexp.MustCompile(`^([$>%#]|>>>|.*[>$#%] )`)

type assistantFormatCaptureOptions struct {
	StripANSI  bool
	LastAnswer bool
	Trim       bool
}

func cmdAssistantFormatCapture(w, wErr io.Writer, gf GlobalFlags, args []string, version string) int {
	_ = version

	fs := newFlagSet("assistant format-capture")
	stripANSI := fs.Bool("strip-ansi", true, "remove ANSI escape sequences")
	lastAnswer := fs.Bool("last-answer", false, "extract only the last answer block")
	trim := fs.Bool("trim", false, "trim leading/trailing empty lines")
	if err := fs.Parse(args); err != nil {
		return returnUsageError(w, wErr, gf, assistantFormatCaptureUsage, version, err)
	}
	if len(fs.Args()) > 0 {
		return returnUsageError(
			w, wErr, gf, assistantFormatCaptureUsage, version,
			fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " ")),
		)
	}

	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		if gf.JSON {
			ReturnError(w, "read_failed", err.Error(), nil, version)
		} else {
			Errorf(wErr, "failed to read stdin: %v", err)
		}
		return ExitInternalError
	}

	output := assistantFormatCapture(string(input), assistantFormatCaptureOptions{
		StripANSI:  *stripANSI,
		LastAnswer: *lastAnswer,
		Trim:       *trim,
	})
	if assistantWriteContent(w, output) != nil {
		return ExitInternalError
	}
	return ExitOK
}

func assistantFormatCapture(input string, opts assistantFormatCaptureOptions) string {
	output := input
	if opts.StripANSI {
		output = assistantStripANSIAndCarriageReturns(output)
	}
	if opts.LastAnswer {
		output = assistantExtractLastAnswer(output)
	}
	if opts.Trim {
		output = assistantTrimEdgeEmptyLines(output)
	}
	return output
}

func assistantStripANSIAndCarriageReturns(input string) string {
	if strings.Contains(input, "\x1b") {
		lines := strings.Split(input, "\n")
		for i := range lines {
			lines[i] = stripANSIEscape(lines[i])
		}
		input = strings.Join(lines, "\n")
	}
	return strings.ReplaceAll(input, "\r", "")
}

func assistantExtractLastAnswer(input string) string {
	lines := strings.Split(input, "\n")
	lastPromptLine := -1
	for i, line := range lines {
		if assistantLastAnswerPromptRegex.MatchString(line) {
			lastPromptLine = i
		}
	}
	if lastPromptLine >= 0 && lastPromptLine+1 < len(lines) {
		return strings.Join(lines[lastPromptLine+1:], "\n")
	}
	return input
}

func assistantTrimEdgeEmptyLines(input string) string {
	lines := strings.Split(input, "\n")
	start := 0
	for start < len(lines) && lines[start] == "" {
		start++
	}
	end := len(lines)
	for end > start && lines[end-1] == "" {
		end--
	}
	return strings.Join(lines[start:end], "\n")
}
