package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/yypyyd/longhub-manager/internal/manageragent"
)

type agentStreamEvent struct {
	Type     string                     `json:"type"`
	Message  string                     `json:"message,omitempty"`
	Tool     string                     `json:"tool,omitempty"`
	Summary  string                     `json:"summary,omitempty"`
	Round    int                        `json:"round,omitempty"`
	Success  *bool                      `json:"success,omitempty"`
	Delta    string                     `json:"delta,omitempty"`
	Code     string                     `json:"code,omitempty"`
	Response *manageragent.TurnResponse `json:"response,omitempty"`
}

func (s *Server) handleAgentTurnStream(w http.ResponseWriter, r *http.Request) {
	if s.agentEngine == nil || r.Method != http.MethodPost {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED")
		} else {
			writeError(w, http.StatusServiceUnavailable, "MANAGER_AGENT_NOT_CONFIGURED")
		}
		return
	}
	var body struct {
		SessionID string `json:"session_id"`
		Message   string `json:"message"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 12*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil || !jsonEOF(decoder) {
		writeError(w, http.StatusBadRequest, "INVALID_AGENT_TURN")
		return
	}
	streamAgentRun(w, r, func(events manageragent.EventSink) (manageragent.TurnResponse, error) {
		return s.agentEngine.TurnWithEvents(r.Context(), body.SessionID, body.Message, events)
	}, "MANAGER_AGENT_TURN_FAILED")
}

func (s *Server) handleAgentApprovalStream(w http.ResponseWriter, r *http.Request) {
	if s.agentEngine == nil || r.Method != http.MethodPost {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED")
		} else {
			writeError(w, http.StatusServiceUnavailable, "MANAGER_AGENT_NOT_CONFIGURED")
		}
		return
	}
	var body struct {
		SessionID  string `json:"session_id"`
		ApprovalID string `json:"approval_id"`
		Approved   bool   `json:"approved"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil || body.SessionID == "" || body.ApprovalID == "" || !jsonEOF(decoder) {
		writeError(w, http.StatusBadRequest, "INVALID_AGENT_APPROVAL")
		return
	}
	streamAgentRun(w, r, func(events manageragent.EventSink) (manageragent.TurnResponse, error) {
		return s.agentEngine.ResolveApprovalWithEvents(r.Context(), body.SessionID, body.ApprovalID, body.Approved, events)
	}, "MANAGER_AGENT_APPROVAL_FAILED")
}

func streamAgentRun(
	w http.ResponseWriter,
	r *http.Request,
	run func(manageragent.EventSink) (manageragent.TurnResponse, error),
	errorCode string,
) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "AGENT_STREAM_UNAVAILABLE")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	streamOpen := true
	answerStarted := false
	writeEvent := func(event agentStreamEvent) {
		if !streamOpen {
			return
		}
		encoded, err := json.Marshal(event)
		if err != nil {
			streamOpen = false
			return
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", encoded); err != nil {
			streamOpen = false
			return
		}
		flusher.Flush()
	}
	events := func(event manageragent.TurnEvent) {
		if event.Type == "answer_started" {
			answerStarted = true
		}
		writeEvent(agentStreamEvent{
			Type: event.Type, Message: event.Message, Tool: event.Tool, Summary: event.Summary,
			Round: event.Round, Success: event.Success,
		})
	}
	response, err := run(events)
	if err != nil {
		writeEvent(agentStreamEvent{Type: "error", Code: errorCode, Message: manageragent.PublicErrorMessage(err)})
		return
	}
	if response.Reply != "" {
		if !answerStarted {
			writeEvent(agentStreamEvent{Type: "answer_started", Message: "正在生成答复"})
		}
		streamAgentReply(r.Context(), response.Reply, writeEvent)
	}
	response.Reply = ""
	writeEvent(agentStreamEvent{Type: "done", Response: &response})
}

func streamAgentReply(ctx context.Context, reply string, writeEvent func(agentStreamEvent)) {
	runes := []rune(reply)
	if len(runes) == 0 {
		return
	}
	chunkSize := (len(runes) + 159) / 160
	if chunkSize < 1 {
		chunkSize = 1
	}
	for start := 0; start < len(runes); start += chunkSize {
		end := start + chunkSize
		if end > len(runes) {
			end = len(runes)
		}
		writeEvent(agentStreamEvent{Type: "reply_delta", Delta: string(runes[start:end])})
		if end == len(runes) {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(12 * time.Millisecond):
		}
	}
}
