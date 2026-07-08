package mcpstdio

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	SessionID string
	Socket    string
	AuthToken string
}

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
		if err := writeMessage(output, response); err != nil {
			return err
		}
	}
}

func (c Config) authToken() string {
	if c.AuthToken != "" {
		return c.AuthToken
	}
	return os.Getenv("ANYCODE_MCP_TOKEN")
}

func (c Config) forward(ctx context.Context, message []byte) ([]byte, error) {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "unix", c.Socket)
		},
	}
	client := &http.Client{Transport: transport}
	endpoint := "http://anycode/mcp/sessions/" + url.PathEscape(c.SessionID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(message))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
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
