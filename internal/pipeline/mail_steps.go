package pipeline

import (
	"fmt"
	"strings"
	"time"
)

// MailSendStep describes a first-class MCP Agent Mail send operation.
type MailSendStep struct {
	ProjectKey  string       `yaml:"project_key,omitempty" toml:"project_key,omitempty" json:"project_key,omitempty"`
	AgentName   string       `yaml:"agent_name,omitempty" toml:"agent_name,omitempty" json:"agent_name,omitempty"`
	To          StringOrList `yaml:"to,omitempty" toml:"to,omitempty" json:"to,omitempty"`
	Subject     string       `yaml:"subject,omitempty" toml:"subject,omitempty" json:"subject,omitempty"`
	Body        string       `yaml:"body,omitempty" toml:"body,omitempty" json:"body,omitempty"`
	ThreadID    string       `yaml:"thread_id,omitempty" toml:"thread_id,omitempty" json:"thread_id,omitempty"`
	AckRequired bool         `yaml:"ack_required,omitempty" toml:"ack_required,omitempty" json:"ack_required,omitempty"`
}

// FileReservationPathsStep describes an MCP Agent Mail file reservation
// acquisition operation.
type FileReservationPathsStep struct {
	ProjectKey string       `yaml:"project_key,omitempty" toml:"project_key,omitempty" json:"project_key,omitempty"`
	AgentName  string       `yaml:"agent_name,omitempty" toml:"agent_name,omitempty" json:"agent_name,omitempty"`
	Paths      StringOrList `yaml:"paths,omitempty" toml:"paths,omitempty" json:"paths,omitempty"`
	TTLSeconds int          `yaml:"ttl_seconds,omitempty" toml:"ttl_seconds,omitempty" json:"ttl_seconds,omitempty"`
	Exclusive  bool         `yaml:"exclusive,omitempty" toml:"exclusive,omitempty" json:"exclusive,omitempty"`
	Reason     string       `yaml:"reason,omitempty" toml:"reason,omitempty" json:"reason,omitempty"`
}

// MailInboxCheckStep describes a first-class MCP Agent Mail inbox polling
// operation.
type MailInboxCheckStep struct {
	ProjectKey    string `yaml:"project_key,omitempty" toml:"project_key,omitempty" json:"project_key,omitempty"`
	AgentName     string `yaml:"agent_name,omitempty" toml:"agent_name,omitempty" json:"agent_name,omitempty"`
	UntilAckCount int    `yaml:"until_ack_count,omitempty" toml:"until_ack_count,omitempty" json:"until_ack_count,omitempty"`
}

// FileReservationReleaseStep describes an MCP Agent Mail file reservation
// release operation.
type FileReservationReleaseStep struct {
	ProjectKey string       `yaml:"project_key,omitempty" toml:"project_key,omitempty" json:"project_key,omitempty"`
	AgentName  string       `yaml:"agent_name,omitempty" toml:"agent_name,omitempty" json:"agent_name,omitempty"`
	Paths      StringOrList `yaml:"paths,omitempty" toml:"paths,omitempty" json:"paths,omitempty"`
}

// hasMailStep reports whether the step is configured as any of the Agent Mail
// dispatch kinds. The runtime executor uses this to short-circuit the prompt
// dispatch path so authors get a structured "not implemented" skip instead of
// a misleading "step has no prompt or prompt_file" failure (bd-hz1tl).
func (s *Step) hasMailStep() bool {
	if s == nil {
		return false
	}
	return s.MailSend != nil ||
		s.FileReservationPaths != nil ||
		s.MailInboxCheck != nil ||
		s.FileReservationRelease != nil
}

// executeMailStep is the executor dispatch branch for Agent Mail step kinds.
// MCP Agent Mail integration is pending (bd-b5l8d follow-on); until then the
// step is recorded as Skipped with SkipKindNotImplemented and a SkipReason
// naming the kinds the author requested.
func executeMailStep(step *Step) StepResult {
	now := time.Now()
	result := StepResult{
		StepID:     step.ID,
		Status:     StatusSkipped,
		StartedAt:  now,
		FinishedAt: now,
		SkipKind:   SkipKindNotImplemented,
		SkipReason: fmt.Sprintf("Agent Mail step kinds (%s) are validated but not yet executed; pending MCP integration.",
			strings.Join(step.mailStepKindNames(), ",")),
	}
	return result
}

func (s *Step) mailStepKindNames() []string {
	if s == nil {
		return nil
	}

	var names []string
	if s.MailSend != nil {
		names = append(names, "mail_send")
	}
	if s.FileReservationPaths != nil {
		names = append(names, "file_reservation_paths")
	}
	if s.MailInboxCheck != nil {
		names = append(names, "mail_inbox_check")
	}
	if s.FileReservationRelease != nil {
		names = append(names, "file_reservation_release")
	}
	return names
}

// validateMailStepPayload reports per-kind required-field errors for the
// Agent Mail step kinds (bd-vv7ij). Mutual-exclusion checks live in
// Validate; this only fires when exactly one mail step kind is set.
func validateMailStepPayload(step *Step, stepField string, result *ValidationResult) {
	if step == nil {
		return
	}

	if send := step.MailSend; send != nil {
		field := stepField + ".mail_send"
		if strings.TrimSpace(send.ProjectKey) == "" {
			result.addError(ParseError{
				Field:   field + ".project_key",
				Message: "mail_send requires project_key",
				Hint:    "Set the absolute project root (e.g. /data/projects/ntm) so MCP Agent Mail can scope the send.",
			})
		}
		if strings.TrimSpace(send.AgentName) == "" {
			result.addError(ParseError{
				Field:   field + ".agent_name",
				Message: "mail_send requires agent_name",
				Hint:    "Set agent_name to the sender's roster name (e.g. TealCrane).",
			})
		}
		if len(send.To) == 0 {
			result.addError(ParseError{
				Field:   field + ".to",
				Message: "mail_send requires at least one recipient in to",
				Hint:    "Use a string for a single recipient or a list for multiple recipients.",
			})
		} else {
			for i, recipient := range send.To {
				if strings.TrimSpace(recipient) == "" {
					result.addError(ParseError{
						Field:   fmt.Sprintf("%s.to[%d]", field, i),
						Message: "mail_send recipient cannot be empty",
					})
				}
			}
		}
		if strings.TrimSpace(send.Subject) == "" && strings.TrimSpace(send.Body) == "" {
			result.addError(ParseError{
				Field:   field,
				Message: "mail_send requires subject or body",
				Hint:    "Set subject or body so the recipient has something to read.",
			})
		}
	}

	if reserve := step.FileReservationPaths; reserve != nil {
		field := stepField + ".file_reservation_paths"
		if strings.TrimSpace(reserve.ProjectKey) == "" {
			result.addError(ParseError{
				Field:   field + ".project_key",
				Message: "file_reservation_paths requires project_key",
			})
		}
		if strings.TrimSpace(reserve.AgentName) == "" {
			result.addError(ParseError{
				Field:   field + ".agent_name",
				Message: "file_reservation_paths requires agent_name",
			})
		}
		if len(reserve.Paths) == 0 {
			result.addError(ParseError{
				Field:   field + ".paths",
				Message: "file_reservation_paths requires at least one path",
				Hint:    "Use a string for a single path or a list for multiple paths.",
			})
		} else {
			for i, path := range reserve.Paths {
				if strings.TrimSpace(path) == "" {
					result.addError(ParseError{
						Field:   fmt.Sprintf("%s.paths[%d]", field, i),
						Message: "file_reservation_paths path cannot be empty",
					})
				}
			}
		}
		if reserve.TTLSeconds < 0 {
			result.addError(ParseError{
				Field:   field + ".ttl_seconds",
				Message: fmt.Sprintf("file_reservation_paths.ttl_seconds must be non-negative, got %d", reserve.TTLSeconds),
				Hint:    "Use 0 for the server default or a positive integer for the lock TTL in seconds.",
			})
		}
	}

	if inbox := step.MailInboxCheck; inbox != nil {
		field := stepField + ".mail_inbox_check"
		if strings.TrimSpace(inbox.ProjectKey) == "" {
			result.addError(ParseError{
				Field:   field + ".project_key",
				Message: "mail_inbox_check requires project_key",
			})
		}
		if strings.TrimSpace(inbox.AgentName) == "" {
			result.addError(ParseError{
				Field:   field + ".agent_name",
				Message: "mail_inbox_check requires agent_name",
			})
		}
		if inbox.UntilAckCount < 0 {
			result.addError(ParseError{
				Field:   field + ".until_ack_count",
				Message: fmt.Sprintf("mail_inbox_check.until_ack_count must be non-negative, got %d", inbox.UntilAckCount),
			})
		}
	}

	if release := step.FileReservationRelease; release != nil {
		field := stepField + ".file_reservation_release"
		if strings.TrimSpace(release.ProjectKey) == "" {
			result.addError(ParseError{
				Field:   field + ".project_key",
				Message: "file_reservation_release requires project_key",
			})
		}
		if strings.TrimSpace(release.AgentName) == "" {
			result.addError(ParseError{
				Field:   field + ".agent_name",
				Message: "file_reservation_release requires agent_name",
			})
		}
		if len(release.Paths) == 0 {
			result.addError(ParseError{
				Field:   field + ".paths",
				Message: "file_reservation_release requires at least one path",
				Hint:    "Use a string for a single path or a list for multiple paths.",
			})
		} else {
			for i, path := range release.Paths {
				if strings.TrimSpace(path) == "" {
					result.addError(ParseError{
						Field:   fmt.Sprintf("%s.paths[%d]", field, i),
						Message: "file_reservation_release path cannot be empty",
					})
				}
			}
		}
	}
}
