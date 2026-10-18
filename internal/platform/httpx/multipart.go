package httpx

import (
	"errors"
	"mime/multipart"
	"net/http"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/apperr"
)

// Multipart parses a multipart/form-data request body with a per-route byte
// limit (the global JSON limit does not apply to multipart routes).
func Multipart(w http.ResponseWriter, r *http.Request, maxBytes int64) (*multipart.Form, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	if err := r.ParseMultipartForm(maxBytes); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return nil, apperr.New(apperr.RequestEntityTooLarge, "The uploaded payload is too large.")
		}
		if errors.Is(err, http.ErrNotMultipart) {
			return nil, apperr.New(apperr.UnsupportedMediaType, "The request must be multipart/form-data.")
		}
		return nil, apperr.New(apperr.MalformedRequest, "The multipart payload is malformed.")
	}
	return r.MultipartForm, nil
}

// FormFile returns the single uploaded file for a form field, or a validation
// error when the field is missing.
func FormFile(form *multipart.Form, field string) (*multipart.FileHeader, error) {
	if form == nil || len(form.File[field]) == 0 {
		return nil, apperr.Validation(map[string][]string{
			field: {"The " + field + " field is required."},
		})
	}
	return form.File[field][0], nil
}
