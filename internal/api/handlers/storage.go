package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/ruaandeysel/vault/internal/db"
	"github.com/ruaandeysel/vault/internal/storage"
)

type StorageHandler struct {
	db *db.DB
}

func NewStorageHandler(database *db.DB) *StorageHandler {
	return &StorageHandler{db: database}
}

func (h *StorageHandler) List(w http.ResponseWriter, r *http.Request) {
	dests, err := h.db.ListStorageDestinations()
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, dests)
}

func (h *StorageHandler) Create(w http.ResponseWriter, r *http.Request) {
	var dest db.StorageDestination
	if err := json.NewDecoder(r.Body).Decode(&dest); err != nil {
		respondError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	id, err := h.db.CreateStorageDestination(dest)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	dest.ID = id
	respondJSON(w, http.StatusCreated, dest)
}

func (h *StorageHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	dest, err := h.db.GetStorageDestination(id)
	if err != nil {
		respondError(w, http.StatusNotFound, "not found")
		return
	}
	respondJSON(w, http.StatusOK, dest)
}

func (h *StorageHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var dest db.StorageDestination
	if err := json.NewDecoder(r.Body).Decode(&dest); err != nil {
		respondError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	dest.ID = id
	if err := h.db.UpdateStorageDestination(dest); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, dest)
}

func (h *StorageHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err := h.db.DeleteStorageDestination(id); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *StorageHandler) TestConnection(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	dest, err := h.db.GetStorageDestination(id)
	if err != nil {
		respondError(w, http.StatusNotFound, "not found")
		return
	}
	adapter, err := storage.NewAdapter(dest.Type, dest.Config)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := adapter.TestConnection(); err != nil {
		respondJSON(w, http.StatusOK, map[string]any{"success": false, "error": err.Error()})
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"success": true})
}

func respondJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func respondError(w http.ResponseWriter, status int, msg string) {
	respondJSON(w, status, map[string]string{"error": msg})
}
