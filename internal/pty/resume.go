package pty

import (
	"fmt"
	"strings"

	"github.com/andyrewlee/amux/internal/data"
)

// DefaultResumeInfo returns the default resume strategy for an assistant.
func DefaultResumeInfo(agentType AgentType) data.ResumeInfo {
	switch agentType {
	case AgentCodex:
		return data.ResumeInfo{Mode: data.ResumeModeLast}
	case AgentClaude, AgentGemini, AgentAmp, AgentOpencode:
		return data.ResumeInfo{Mode: data.ResumeModeContinue}
	default:
		return data.ResumeInfo{}
	}
}

// ApplyResumeCommand converts a base command into a resume command when supported.
func ApplyResumeCommand(base string, agentType AgentType, resume data.ResumeInfo) string {
	base = strings.TrimSpace(base)
	if base == "" || resume.Mode == data.ResumeModeNone {
		return base
	}

	switch agentType {
	case AgentCodex:
		switch resume.Mode {
		case data.ResumeModeID:
			if resume.ID != "" {
				return fmt.Sprintf("%s resume %s", base, resume.ID)
			}
		case data.ResumeModeLast, data.ResumeModeContinue:
			return fmt.Sprintf("%s resume --last", base)
		}
	case AgentClaude:
		switch resume.Mode {
		case data.ResumeModeID:
			if resume.ID != "" {
				return fmt.Sprintf("%s --resume %s", base, resume.ID)
			}
		case data.ResumeModeLast, data.ResumeModeContinue:
			return fmt.Sprintf("%s --continue", base)
		}
	case AgentGemini:
		switch resume.Mode {
		case data.ResumeModeID, data.ResumeModeIndex:
			if resume.ID != "" {
				return fmt.Sprintf("%s --resume %s", base, resume.ID)
			}
		case data.ResumeModeLast, data.ResumeModeContinue:
			return fmt.Sprintf("%s --resume", base)
		}
	case AgentAmp:
		switch resume.Mode {
		case data.ResumeModeID:
			if resume.ID != "" {
				return fmt.Sprintf("%s threads continue %s", base, resume.ID)
			}
		}
	case AgentOpencode:
		switch resume.Mode {
		case data.ResumeModeID:
			if resume.ID != "" {
				return fmt.Sprintf("%s --session %s", base, resume.ID)
			}
		case data.ResumeModeLast, data.ResumeModeContinue:
			return fmt.Sprintf("%s --continue", base)
		}
	}

	return base
}

// AutoResumeInput returns input to send after startup for interactive resume flows.
func AutoResumeInput(agentType AgentType, resume data.ResumeInfo) string {
	if agentType != AgentAmp {
		return ""
	}
	if resume.Mode == data.ResumeModeContinue || resume.Mode == data.ResumeModeLast {
		if resume.ID == "" {
			return "/continue\n"
		}
	}
	return ""
}
