package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/pikocloud/pikobrain/internal/brain"
	"github.com/pikocloud/pikobrain/internal/providers/types"
	"github.com/pikocloud/pikobrain/internal/utils"
)

const (
	HeaderRole = "X-Role"
	HeaderUser = "X-User"
	QueryUser  = "user"
	QueryRole  = "role"
)

const (
	HeaderRunDuration     = "X-Run-Duration"      // duration in seconds (float)
	HeaderRunInputTokens  = "X-Run-Input-Tokens"  // total input tokens
	HeaderRunOutputTokens = "X-Run-Output-Tokens" // total output tokens
	HeaderRunTotalTokens  = "X-Run-Total-Tokens"  // total "total" tokens
	HeaderRunContext      = "X-Run-Context"       // total number of messages
)

type Server struct {
	Brain   *brain.Brain
	Timeout time.Duration
}

func (srv *Server) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writer.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	messages, err := parseRequest(request)
	if err != nil {
		slog.Error("Failed to parse request", "error", err)
		writer.WriteHeader(http.StatusBadRequest)
		_, _ = writer.Write([]byte(err.Error()))
		return
	}

	ctx, cancel := context.WithTimeout(request.Context(), srv.Timeout)
	defer cancel()

	started := time.Now()
	res, err := srv.Brain.Run(ctx, messages)
	duration := time.Since(started)

	if err != nil {
		slog.Error("Failed to execute request", "error", err)
		writer.WriteHeader(http.StatusInternalServerError)
		_, _ = writer.Write([]byte(err.Error()))
		return
	}

	reply := res.Reply()
	writer.Header().Set("Content-Type", string(reply.Mime))
	writer.Header().Set("Content-Length", strconv.Itoa(len(reply.Data)))
	writer.Header().Set(HeaderRunDuration, strconv.FormatFloat(duration.Seconds(), 'f', -1, 64))
	writer.Header().Set(HeaderRunInputTokens, strconv.Itoa(res.TotalInputTokens()))
	writer.Header().Set(HeaderRunOutputTokens, strconv.Itoa(res.TotalOutputTokens()))
	writer.Header().Set(HeaderRunTotalTokens, strconv.Itoa(res.TotalTokens()))
	writer.Header().Set(HeaderRunContext, strconv.Itoa(len(messages)))
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(reply.Data)
	slog.Info("complete", "duration", duration, "input", res.TotalInputTokens(), "output", res.TotalOutputTokens(), "total", res.TotalTokens())
}

func parseRequest(request *http.Request) ([]types.Message, error) {
	baseRole, err := getRole(request.URL.Query().Get(QueryRole), types.RoleUser)
	if err != nil {
		return nil, fmt.Errorf("get role from request: %w", err)
	}

	baseUser := request.Header.Get(QueryUser)

	contentType := utils.ContentType(request.Header.Get("Content-Type"))
	switch contentType {
	case "multipart/form-data":
		return readMultipart(request, baseUser, baseRole)
	}
	data, err := io.ReadAll(request.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %v", err)
	}
	content, err := parsePayload(contentType, data)
	if err != nil {
		return nil, fmt.Errorf("parse payload: %v", err)
	}

	return []types.Message{{
		Role:    baseRole,
		User:    baseUser,
		Content: content,
	}}, nil
}

func readMultipart(request *http.Request, baseUser string, baseRole types.Role) ([]types.Message, error) {
	reader, err := request.MultipartReader()
	if err != nil {
		return nil, fmt.Errorf("read multipart request: %w", err)
	}

	var ans []types.Message
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read multipart part: %w", err)
		}

		role, err := getRole(part.Header.Get(HeaderRole), baseRole)
		if err != nil {
			return nil, fmt.Errorf("parse role: %w", err)
		}

		user := baseUser
		if v := part.Header.Get(HeaderUser); v != "" {
			user = v
		}

		body, err := io.ReadAll(part)
		if err != nil {
			return nil, fmt.Errorf("read body: %w", err)
		}

		content, err := parsePayload(part.Header.Get("Content-Type"), body)
		if err != nil {
			return nil, fmt.Errorf("parse payload: %w", err)
		}
		ans = append(ans, types.Message{
			Role:    role,
			User:    user,
			Content: content,
		})
	}
	return ans, nil
}

func parsePayload(contentType string, body []byte) (types.Content, error) {
	switch utils.ContentType(contentType) {
	case "text/plain", "":
		return types.Content{Data: body, Mime: types.MIMEText}, nil
	case "application/json":
		return types.Content{Data: body, Mime: types.MIMEJson}, nil
	case "image/png":
		return types.Content{Data: body, Mime: types.MIMEPng}, nil
	case "image/jpeg":
		return types.Content{Data: body, Mime: types.MIMEJpeg}, nil
	case "image/jpg":
		return types.Content{Data: body, Mime: types.MIMEJpg}, nil
	case "image/gif":
		return types.Content{Data: body, Mime: types.MIMEGif}, nil
	case "image/webp":
		return types.Content{Data: body, Mime: types.MIMEWebp}, nil
	case "application/x-www-form-urlencoded":
		value, err := url.QueryUnescape(string(body))
		if err != nil {
			return types.Content{}, fmt.Errorf("parse form urlencoded: %w", err)
		}
		return types.Content{Data: []byte(value), Mime: types.MIMEText}, nil
	default:
		return types.Content{}, fmt.Errorf("unsupported content type: %s", contentType)
	}
}

func getRole(v string, base types.Role) (types.Role, error) {
	if v == "" {
		return base, nil
	}
	return types.ParseRole(v)
}
