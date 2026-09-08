package http

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sessionapp "github.com/nzlov/anycode/internal/application/session"
	"github.com/nzlov/anycode/internal/infra/config"
	"github.com/nzlov/anycode/internal/interfaces/graphql/graph"
)

func TestSideMultipartInputsRequireAuthAndReachUseCase(t *testing.T) {
	for _, continuing := range []bool{false, true} {
		operation, inputType := "startSessionSide", "StartSessionSideInput"
		if continuing {
			operation, inputType = "continueSessionSide", "ContinueSessionSideInput"
		}
		t.Run(operation, func(t *testing.T) {
			usecase := &sideUploadUseCase{}
			handler := NewHandler(config.Config{AccessKey: "secret"}, WithGraphQLUseCases(graph.UseCases{SessionSides: usecase}))
			input := map[string]any{"sessionId": "session-1", "prompt": "look at this", "config": map[string]any{"codexModel": "model-b", "reasoningEffort": "low", "fastMode": false}, "files": []any{nil}}
			if continuing {
				input["codexSessionId"] = "side-1"
			}
			operations, err := json.Marshal(map[string]any{"query": "mutation($input: " + inputType + "!) { " + operation + "(input: $input) { codexSessionId } }", "variables": map[string]any{"input": input}})
			if err != nil {
				t.Fatal(err)
			}
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			if err := writer.WriteField("operations", string(operations)); err != nil {
				t.Fatal(err)
			}
			if err := writer.WriteField("map", `{"0":["variables.input.files.0"]}`); err != nil {
				t.Fatal(err)
			}
			part, err := writer.CreateFormFile("0", "notes.txt")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := io.WriteString(part, "attachment contents"); err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			for _, authenticated := range []bool{false, true} {
				req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body.Bytes()))
				req.Header.Set("Content-Type", writer.FormDataContentType())
				if authenticated {
					req.Header.Set("Authorization", "Bearer secret")
				}
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, req)
				if !authenticated {
					if rec.Code != http.StatusUnauthorized || usecase.input.SessionID != "" {
						t.Fatalf("unauthorized upload = %d", rec.Code)
					}
					continue
				}
				if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), `"errors"`) {
					t.Fatalf("multipart response: %d %s", rec.Code, rec.Body.String())
				}
				got := usecase.input
				if got.Prompt != "look at this" || got.Config == nil || got.Config.CodexModel != "model-b" || got.Config.ReasoningEffort != "low" || got.Config.FastMode || usecase.content != "attachment contents" || len(got.Files) != 1 || got.Files[0].Filename != "notes.txt" {
					t.Fatalf("uploaded Side = %#v, content = %q", got, usecase.content)
				}
				if continuing && got.CodexSessionID != "side-1" {
					t.Fatalf("continuation thread = %q", got.CodexSessionID)
				}
			}
		})
	}
}

type sideUploadUseCase struct {
	sessionapp.SideUseCase
	input   sessionapp.SideInput
	content string
}

func (u *sideUploadUseCase) StartSide(_ context.Context, input sessionapp.SideInput) (sessionapp.SideRunDTO, error) {
	u.input = input
	data, err := io.ReadAll(input.Files[0].Reader)
	u.content = string(data)
	return sessionapp.SideRunDTO{CodexSessionID: "side-1"}, err
}

func (u *sideUploadUseCase) ContinueSide(ctx context.Context, input sessionapp.SideInput) (sessionapp.SideRunDTO, error) {
	return u.StartSide(ctx, input)
}
