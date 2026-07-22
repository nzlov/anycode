package webpush

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	webpush "github.com/SherClockHolmes/webpush-go"
	notificationdomain "github.com/nzlov/anycode/internal/domain/notification"
)

type Client struct{}

func New() Client {
	return Client{}
}

func (Client) GenerateKeys() (notificationdomain.KeyPair, error) {
	privateKey, publicKey, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		return notificationdomain.KeyPair{}, err
	}
	return notificationdomain.KeyPair{PublicKey: publicKey, PrivateKey: privateKey}, nil
}

func (Client) Send(ctx context.Context, configuration notificationdomain.Configuration, subscription notificationdomain.Subscription, payload []byte, ttl int) (notificationdomain.SendResult, error) {
	httpClient, err := httpClientForProxy(configuration.ProxyURL)
	if err != nil {
		return notificationdomain.SendResult{}, fmt.Errorf("configure web push proxy: %w", err)
	}
	options := &webpush.Options{
		Subscriber: configuration.VAPIDSubject, VAPIDPublicKey: configuration.VAPIDPublicKey,
		VAPIDPrivateKey: configuration.VAPIDPrivateKey, TTL: ttl,
	}
	if httpClient != nil {
		options.HTTPClient = httpClient
	}
	response, err := webpush.SendNotificationWithContext(ctx, payload, &webpush.Subscription{
		Endpoint: subscription.Endpoint,
		Keys:     webpush.Keys{P256dh: subscription.P256DH, Auth: subscription.Auth},
	}, options)
	if err != nil {
		return notificationdomain.SendResult{}, err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)
	if response.StatusCode == 0 {
		return notificationdomain.SendResult{}, fmt.Errorf("push service returned an empty status")
	}
	return notificationdomain.SendResult{StatusCode: response.StatusCode}, nil
}

func httpClientForProxy(value string) (*http.Client, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	proxyURL, err := url.Parse(value)
	if err != nil {
		return nil, err
	}
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, errors.New("default HTTP transport is unavailable")
	}
	transport := base.Clone()
	transport.Proxy = http.ProxyURL(proxyURL)
	return &http.Client{Transport: transport}, nil
}
