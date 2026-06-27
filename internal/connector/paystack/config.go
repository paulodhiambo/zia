package paystack

import "os"

func ConfigFromEnv() Config {
	return Config{
		SecretKey:  os.Getenv("PAYSTACK_SECRET_KEY"),
		PublicKey:  os.Getenv("PAYSTACK_PUBLIC_KEY"),
		WebhookKey: os.Getenv("PAYSTACK_WEBHOOK_KEY"),
		Sandbox:    os.Getenv("APP_ENV") != "production",
	}
}
