package wallet

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const maxRequestBody = 1 << 20

type HTTPHandler struct {
	service *Service
	logger  *slog.Logger
}

func NewHTTPHandler(service *Service, logger *slog.Logger) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	handler := &HTTPHandler{service: service, logger: logger}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", handler.health)
	mux.HandleFunc("POST /wallets", handler.createWallet)
	mux.HandleFunc("GET /wallets/{walletID}", handler.getWallet)
	mux.HandleFunc("POST /transfers", handler.createTransfer)
	return mux
}

func (handler *HTTPHandler) health(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]string{"status": "ok"})
}

func (handler *HTTPHandler) createWallet(writer http.ResponseWriter, request *http.Request) {
	var body CreateWalletRequest
	if err := decodeRequest(writer, request, &body); err != nil {
		writeError(writer, http.StatusBadRequest, err.Error())
		return
	}
	wallet, err := handler.service.CreateWallet(request.Context(), body)
	if err != nil {
		handler.writeServiceError(writer, err)
		return
	}
	writeJSON(writer, http.StatusCreated, wallet)
}

func (handler *HTTPHandler) getWallet(writer http.ResponseWriter, request *http.Request) {
	wallet, err := handler.service.GetWallet(request.Context(), request.PathValue("walletID"))
	if err != nil {
		handler.writeServiceError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, wallet)
}

func (handler *HTTPHandler) createTransfer(writer http.ResponseWriter, request *http.Request) {
	var body TransferRequest
	if err := decodeRequest(writer, request, &body); err != nil {
		writeError(writer, http.StatusBadRequest, err.Error())
		return
	}
	result, err := handler.service.CreateTransfer(request.Context(), body)
	if err != nil {
		handler.writeServiceError(writer, err)
		return
	}
	if result.Status == "FAILED" {
		writeJSON(writer, http.StatusUnprocessableEntity, result)
		return
	}
	writeJSON(writer, http.StatusCreated, result)
}

func decodeRequest(
	writer http.ResponseWriter,
	request *http.Request,
	destination any,
) error {
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return errors.New("Content-Type must be application/json")
	}
	request.Body = http.MaxBytesReader(writer, request.Body, maxRequestBody)
	data, err := io.ReadAll(request.Body)
	if err != nil {
		return errors.New("request body could not be read or exceeds 1 MiB")
	}
	return decodeObject(data, destination)
}

func (handler *HTTPHandler) writeServiceError(writer http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalidRequest):
		writeError(writer, http.StatusBadRequest, publicErrorMessage(err))
	case errors.Is(err, ErrIdempotencyReuse), errors.Is(err, ErrConflict):
		writeError(writer, http.StatusConflict, publicErrorMessage(err))
	case errors.Is(err, ErrNotFound):
		writeError(writer, http.StatusNotFound, publicErrorMessage(err))
	default:
		handler.logger.Error("wallet request failed", "error", err)
		writeError(writer, http.StatusInternalServerError, "internal server error")
	}
}

func publicErrorMessage(err error) string {
	message := err.Error()
	for _, prefix := range []string{
		ErrInvalidRequest.Error() + ": ",
		ErrConflict.Error() + ": ",
		ErrIdempotencyReuse.Error() + ": ",
		ErrNotFound.Error() + ": ",
	} {
		message = strings.TrimPrefix(message, prefix)
	}
	return message
}

func writeError(writer http.ResponseWriter, status int, message string) {
	writeJSON(writer, status, map[string]string{"error": message})
}

func writeJSON(writer http.ResponseWriter, status int, body any) {
	encoded, err := json.Marshal(body)
	if err != nil {
		slog.Error("encode HTTP response", "error", err)
		http.Error(writer, "internal server error", http.StatusInternalServerError)
		return
	}
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.Header().Set("Content-Length", stringLength(encoded))
	writer.WriteHeader(status)
	if _, err := writer.Write(encoded); err != nil {
		slog.Error("write HTTP response", "error", err)
	}
}

func stringLength(encoded []byte) string {
	return strconv.Itoa(len(encoded))
}

func NewHTTPServer(address string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              address,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}
