package webpush

import (
	"context"
	"fmt"
	"io"

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
	response, err := webpush.SendNotificationWithContext(ctx, payload, &webpush.Subscription{
		Endpoint: subscription.Endpoint,
		Keys:     webpush.Keys{P256dh: subscription.P256DH, Auth: subscription.Auth},
	}, &webpush.Options{
		Subscriber: configuration.VAPIDSubject, VAPIDPublicKey: configuration.VAPIDPublicKey,
		VAPIDPrivateKey: configuration.VAPIDPrivateKey, TTL: ttl,
	})
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
