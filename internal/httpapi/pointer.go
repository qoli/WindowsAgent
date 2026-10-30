package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	"github.com/qoli/WindowsAgent/internal/pointeraction"
	"github.com/qoli/WindowsAgent/internal/strictjson"
)

func (s *Server) handleDirectPointerInvoke(w http.ResponseWriter, r *http.Request, requestID string) {
	r.Body = http.MaxBytesReader(w, r.Body, maxKeyInputBody)
	defer r.Body.Close()
	data, err := io.ReadAll(r.Body)
	if err == nil {
		err = strictjson.Validate(data)
	}
	var request pointeraction.DirectClickRequest
	if err == nil {
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		err = decoder.Decode(&request)
	}
	if err == nil {
		err = request.Validate()
	}
	if err != nil {
		writeError(w, requestID, http.StatusBadRequest, "invalid_pointer_input_request", err.Error())
		return
	}
	result, err := s.actions.InvokeDirectPointer(r.Context(), request)
	if err != nil {
		s.writeActionError(w, requestID, err)
		return
	}
	w.Header().Set("Location", "/v1/action-invocations/"+result.InvocationID)
	s.logger.InfoContext(r.Context(), "direct_pointer_input_invoked", "request_id", requestID, "invocation_id", result.InvocationID,
		"operation", request.Operation, "expected_foreground_process_id", request.ExpectedForeground.ProcessID, "state", result.State)
	writeJSON(w, http.StatusAccepted, result)
}
