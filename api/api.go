// Package api implements the Go HTTP API surface.
//
// It owns the JSON routes, stable error codes, idempotent operation handling,
// adapter fault-script entry points, logical-clock injection and audit queries
// documented for the PotatoEye service (component traceability: Go HTTP API ->
// api).
package api

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"

	"potatoeye-cutseed-sprout-gate/evidence"
	"potatoeye-cutseed-sprout-gate/store"
	"potatoeye-cutseed-sprout-gate/task"
)

// ErrorResponse is the stable JSON error envelope (failure boundary 3).
type ErrorResponse struct {
	Code    store.Code `json:"code"`
	Message string     `json:"message"`
	Reasons []string   `json:"reasons,omitempty"`
}

// Server is the HTTP handler holding the store/service boundary.
type Server struct {
	store store.Store
	mux   *http.ServeMux
}

// NewServer builds a Server with the documented route table registered.
func NewServer(s store.Store) *Server {
	srv := &Server{store: s, mux: http.NewServeMux()}
	srv.routes()
	return srv
}

// Handler returns the root HTTP handler.
func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", s.handleHealth)

	s.mux.HandleFunc("POST /v1/tasks/lock", s.handleLock)
	s.mux.HandleFunc("POST /v1/tasks/{id}/cut-confirmations", s.handleCutConfirmations)
	s.mux.HandleFunc("POST /v1/tasks/{id}/sample-seals", s.handleSampleSeals)
	s.mux.HandleFunc("POST /v1/tasks/{id}/observations/{age_day}", s.handleObservations)
	s.mux.HandleFunc("POST /v1/tasks/{id}/bud-eyes/verify", s.handleBudEyes)
	s.mux.HandleFunc("POST /v1/tasks/{id}/disease-evidence", s.handleDiseaseEvidence)
	s.mux.HandleFunc("POST /v1/tasks/{id}/reviews", s.handleReviews)
	s.mux.HandleFunc("POST /v1/tasks/{id}/finalize", s.handleFinalize)
	s.mux.HandleFunc("GET /v1/tasks/{id}", s.handleGetTask)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleLock(w http.ResponseWriter, r *http.Request) {
	var req store.LockRequest
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, store.CodeInvalidRequest, err.Error())
		return
	}
	res, err := s.store.Lock(r.Context(), req)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, res)
}

func (s *Server) handleCutConfirmations(w http.ResponseWriter, r *http.Request) {
	var req store.CutConfirmRequest
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, store.CodeInvalidRequest, err.Error())
		return
	}
	res, err := s.store.CutConfirm(r.Context(), task.ID(r.PathValue("id")), req)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleSampleSeals(w http.ResponseWriter, r *http.Request) {
	var req store.SampleSealRequest
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, store.CodeInvalidRequest, err.Error())
		return
	}
	res, err := s.store.SampleSeal(r.Context(), task.ID(r.PathValue("id")), req)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleObservations(w http.ResponseWriter, r *http.Request) {
	ageDay, err := strconv.Atoi(r.PathValue("age_day"))
	if err != nil {
		writeError(w, http.StatusBadRequest, store.CodeInvalidRequest, "invalid age_day")
		return
	}
	var req store.ObserveRequest
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, store.CodeInvalidRequest, err.Error())
		return
	}
	res, err := s.store.Observe(r.Context(), task.ID(r.PathValue("id")), evidence.AgeDay(ageDay), req)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleBudEyes(w http.ResponseWriter, r *http.Request) {
	var req store.BudEyeRequest
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, store.CodeInvalidRequest, err.Error())
		return
	}
	res, err := s.store.VerifyBudEyes(r.Context(), task.ID(r.PathValue("id")), req)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleDiseaseEvidence(w http.ResponseWriter, r *http.Request) {
	var req store.DiseaseRequest
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, store.CodeInvalidRequest, err.Error())
		return
	}
	res, err := s.store.AddDiseaseEvidence(r.Context(), task.ID(r.PathValue("id")), req)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleReviews(w http.ResponseWriter, r *http.Request) {
	var req store.ReviewRequest
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, store.CodeInvalidRequest, err.Error())
		return
	}
	res, err := s.store.Review(r.Context(), task.ID(r.PathValue("id")), req)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleFinalize(w http.ResponseWriter, r *http.Request) {
	var req store.FinalizeRequest
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, store.CodeInvalidRequest, err.Error())
		return
	}
	res, err := s.store.FinalizeTask(r.Context(), task.ID(r.PathValue("id")), req)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleGetTask(w http.ResponseWriter, r *http.Request) {
	detail, err := s.store.GetTask(r.Context(), task.ID(r.PathValue("id")))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func decode(r *http.Request, v any) error {
	return json.NewDecoder(r.Body).Decode(v)
}

func writeStoreError(w http.ResponseWriter, err error) {
	if se, ok := err.(*store.Error); ok {
		writeError(w, httpStatusFor(se.Code), se.Code, se.Msg)
		return
	}
	writeError(w, http.StatusInternalServerError, store.CodeInvalidRequest, err.Error())
}

func httpStatusFor(c store.Code) int {
	switch c {
	case store.CodeNotFound:
		return http.StatusNotFound
	case store.CodeConflict, store.CodeStaleGeneration, store.CodeTerminal,
		store.CodeDuplicateKey, store.CodeNotFinalizable:
		return http.StatusConflict
	case store.CodeAdapterRetry:
		return http.StatusAccepted
	default:
		return http.StatusBadRequest
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("api: encode response: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, code store.Code, msg string) {
	writeJSON(w, status, ErrorResponse{Code: code, Message: msg})
}
