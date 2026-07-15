package response

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

const MaxManagerJSONBodyBytes int64 = 4 * 1024 * 1024

type JSONDecodeOptions struct {
	MaxBytes              int64
	AllowEmpty            bool
	DisallowUnknownFields bool
}

func DecodeJSON(w http.ResponseWriter, r *http.Request, target any, options JSONDecodeOptions) bool {
	maxBytes := options.MaxBytes
	if maxBytes <= 0 {
		maxBytes = MaxManagerJSONBodyBytes
	}
	if r.ContentLength > maxBytes {
		Error(w, http.StatusRequestEntityTooLarge, errors.New("http: request body too large"))
		return false
	}
	body := http.MaxBytesReader(w, r.Body, maxBytes)
	decoder := json.NewDecoder(body)
	if options.DisallowUnknownFields {
		decoder.DisallowUnknownFields()
	}
	if err := decoder.Decode(target); err != nil {
		if errors.Is(err, io.EOF) && options.AllowEmpty {
			return true
		}
		writeJSONDecodeError(w, err)
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			Error(w, http.StatusBadRequest, errors.New("request body must contain a single JSON value"))
			return false
		}
		writeJSONDecodeError(w, err)
		return false
	}
	return true
}

func writeJSONDecodeError(w http.ResponseWriter, err error) {
	var maxBytesErr *http.MaxBytesError
	if errors.As(err, &maxBytesErr) {
		Error(w, http.StatusRequestEntityTooLarge, err)
		return
	}
	Error(w, http.StatusBadRequest, err)
}
