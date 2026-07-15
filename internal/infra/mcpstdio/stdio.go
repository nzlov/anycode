package mcpstdio

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	SessionID string
	Socket    string
	AuthToken string
}

const deliveryCommandTimeout = 10 * time.Second

func Run(ctx context.Context, input io.Reader, output io.Writer, cfg Config) error {
	if strings.TrimSpace(cfg.SessionID) == "" {
		return errors.New("session id is required")
	}
	if strings.TrimSpace(cfg.Socket) == "" {
		return errors.New("mcp socket is required")
	}
	reader := bufio.NewReader(input)
	for {
		message, err := readMessage(reader)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		response, err := cfg.forward(ctx, message)
		if err != nil {
			return err
		}
		if len(bytes.TrimSpace(response)) == 0 {
			continue
		}
		batchID := directAnswerBatchID(response)
		if err := writeMessage(output, response); err != nil {
			if batchID != "" {
				return errors.Join(err, cfg.failDelivery(context.WithoutCancel(ctx), batchID))
			}
			return err
		}
		if batchID != "" {
			// GLUE: this ACK bridges the stdio write boundary to direct answer delivery; remove if Codex exposes a native post-delivery hook.
			if err := cfg.acknowledgeDelivery(ctx, batchID); err != nil {
				return errors.Join(err, cfg.failDelivery(context.WithoutCancel(ctx), batchID))
			}
		}
	}
}

func (c Config) failDelivery(ctx context.Context, batchID string) error {
	return c.deliveryCommand(ctx, batchID, "fail")
}

func (c Config) authToken() string {
	if c.AuthToken != "" {
		return c.AuthToken
	}
	return os.Getenv("ANYCODE_MCP_TOKEN")
}

func (c Config) forward(ctx context.Context, message []byte) ([]byte, error) {
	client := c.httpClient()
	endpoint := "http://anycode/mcp/sessions/" + url.PathEscape(c.SessionID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(message))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-AnyCode-MCP-Transport", "stdio")
	if token := c.authToken(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("mcp http status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return body, nil
}

func (c Config) acknowledgeDelivery(ctx context.Context, batchID string) error {
	return c.deliveryCommand(ctx, batchID, "ack")
}

func (c Config) deliveryCommand(ctx context.Context, batchID string, action string) error {
	commandCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), deliveryCommandTimeout)
	defer cancel()
	client := c.httpClient()
	endpoint := "http://anycode/mcp/sessions/" + url.PathEscape(c.SessionID) + "/deliveries/" + url.PathEscape(batchID) + "/" + url.PathEscape(action)
	req, err := http.NewRequestWithContext(commandCtx, http.MethodPost, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-AnyCode-MCP-Transport", "stdio")
	if token := c.authToken(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("mcp delivery %s status %d: %s", action, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func (c Config) httpClient() *http.Client {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "unix", c.Socket)
		},
	}
	return &http.Client{Transport: transport}
}

func directAnswerBatchID(response []byte) string {
	var envelope struct {
		Result struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if json.Unmarshal(response, &envelope) != nil {
		return ""
	}
	for _, content := range envelope.Result.Content {
		if content.Type != "text" {
			continue
		}
		var delivery struct {
			BatchID string `json:"batchId"`
			Status  string `json:"status"`
		}
		if json.Unmarshal([]byte(content.Text), &delivery) == nil && delivery.Status == "answered" {
			return strings.TrimSpace(delivery.BatchID)
		}
	}
	return ""
}

func readMessage(reader *bufio.Reader) ([]byte, error) {
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) && strings.TrimSpace(line) != "" {
				return []byte(strings.TrimSpace(line)), nil
			}
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			continue
		}
		if length, ok, err := contentLength(line); ok || err != nil {
			if err != nil {
				return nil, err
			}
			for {
				header, err := reader.ReadString('\n')
				if err != nil {
					return nil, err
				}
				if strings.TrimSpace(header) == "" {
					break
				}
			}
			message := make([]byte, length)
			_, err := io.ReadFull(reader, message)
			return message, err
		}
		return []byte(strings.TrimSpace(line)), nil
	}
}

func contentLength(line string) (int, bool, error) {
	name, value, ok := strings.Cut(line, ":")
	if !ok || !strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
		return 0, false, nil
	}
	length, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || length < 0 {
		return 0, true, fmt.Errorf("invalid content length %q", value)
	}
	return length, true, nil
}

func writeMessage(output io.Writer, message []byte) error {
	_, err := fmt.Fprintf(output, "Content-Length: %d\r\n\r\n%s", len(message), message)
	return err
}
