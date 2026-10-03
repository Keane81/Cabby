package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Keane81/Cabby/backend/dbviewer/internal/query"
	"github.com/Keane81/Cabby/backend/dbviewer/internal/store"
)

// apiError is a fixed answer: the message never carries a cause, a value from the request or any
// text that came from the database.
type apiError struct {
	status  int
	code    string
	message string
}

var (
	errInvalidFilter = apiError{http.StatusBadRequest, query.CodeInvalidFilter, "Некорректный поиск или фильтр"}
	errInvalidSort   = apiError{http.StatusBadRequest, query.CodeInvalidSort, "Некорректная сортировка"}
	errInvalidPage   = apiError{http.StatusBadRequest, query.CodeInvalidPage, "Некорректный номер или размер страницы"}
	errUnknownTable  = apiError{http.StatusNotFound, "unknown_table", "База или таблица не найдена"}
	errNotFound      = apiError{http.StatusNotFound, "not_found", "Не найдено"}
	errMethod        = apiError{http.StatusMethodNotAllowed, "method_not_allowed", "Допустим только GET"}
	errUnavailable   = apiError{http.StatusServiceUnavailable, "database_unavailable", "База данных недоступна"}
	errTimeout       = apiError{http.StatusGatewayTimeout, "query_timeout", "Запрос выполняется слишком долго: сузьте условия"}
	errInternal      = apiError{http.StatusInternalServerError, "internal", "Внутренняя ошибка"}
)

// fromError maps what the store and the query builder return to an answer.
func fromError(err error) apiError {
	var qe *query.Error
	switch {
	case errors.As(err, &qe):
		switch qe.Code {
		case query.CodeInvalidSort:
			return errInvalidSort
		case query.CodeInvalidPage:
			return errInvalidPage
		default:
			return errInvalidFilter
		}
	case errors.Is(err, store.ErrUnknownTable):
		return errUnknownTable
	case errors.Is(err, store.ErrUnavailable):
		return errUnavailable
	case errors.Is(err, store.ErrTimeout):
		return errTimeout
	default:
		return errInternal
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, e apiError) {
	writeJSON(w, e.status, map[string]any{"error": map[string]string{"code": e.code, "message": e.message}})
}
